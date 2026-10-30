package parser

import (
	"errors"
	"fmt"
	"time"
)

var ErrImportedCandidateResolution = errors.New("imported candidate requires guarded import and relevance resolution")

type CandidateStageRole struct {
	ID                      int64
	PromptVersion, ModelKey string
	Decision                *string
}
type CandidateStageSnapshot struct{ ID, FetchedAt string }
type CandidateStageLeaf struct {
	ID, InputHash string
	RoleIDs       []int64
}
type CandidateStageInput struct {
	CandidateID, TitleRevision                                                 int64
	CompanyID, AgencyID                                                        *int64
	SourceID, TitleHash, TitleUpdatedAt                                        string
	SourceSelected, ScopeAllowed                                               bool
	Roles                                                                      []CandidateStageRole
	Snapshot                                                                   *CandidateStageSnapshot
	LastSuccessfulFetch                                                        *int64
	ExtractionID, ExtractionConfigHash, ExtractionPromptVersion, LeafInputHash string
	Leaf                                                                       *CandidateStageLeaf
	Imported                                                                   bool
}
type CandidateStagePlan struct {
	Type        string         `json:"type"`
	Key         string         `json:"key"`
	CompanyID   *int64         `json:"companyId"`
	AgencyID    *int64         `json:"agencyId"`
	CandidateID int64          `json:"candidateId"`
	SourceID    string         `json:"sourceId"`
	Payload     map[string]any `json:"payload"`
}

func stageDateMillis(value string) (int64, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02", "2006-01-02 15:04:05"} {
		v, e := time.Parse(layout, value)
		if e == nil {
			return v.UnixMilli(), true
		}
	}
	return 0, false
}

// Чистый расчёт работает только с проверенным серверным snapshot. Сам он не
// читает job payload, не создаёт заданий и не вызывает внешние сервисы.
func PlanCandidateStage(in CandidateStageInput) (*CandidateStagePlan, error) {
	if !in.SourceSelected || !in.ScopeAllowed {
		return nil, nil
	}
	if in.CandidateID < 1 || in.CandidateID > 9007199254740991 || in.TitleRevision < 1 || in.TitleRevision > 9007199254740991 || in.TitleHash == "" || in.SourceID == "" || (in.CompanyID == nil) == (in.AgencyID == nil) {
		return nil, ErrJobInput
	}
	if in.CompanyID != nil && (*in.CompanyID < 1 || *in.CompanyID > 9007199254740991) || in.AgencyID != nil && (*in.AgencyID < 1 || *in.AgencyID > 9007199254740991) {
		return nil, ErrJobInput
	}
	if len(in.Roles) == 0 {
		return nil, nil
	}
	roleVersions := make([][]any, 0, len(in.Roles))
	seen := map[int64]bool{}
	missing, matched := false, false
	for _, r := range in.Roles {
		if r.ID < 1 || r.ID > 9007199254740991 || r.PromptVersion == "" || r.ModelKey == "" || seen[r.ID] {
			return nil, ErrJobInput
		}
		seen[r.ID] = true
		roleVersions = append(roleVersions, []any{r.ID, r.PromptVersion, r.ModelKey})
		if r.Decision == nil || *r.Decision == "" {
			missing = true
			continue
		}
		switch *r.Decision {
		case "match":
			matched = true
		case "reject":
		default:
			return nil, ErrJobInput
		}
	}
	version, e := canonicalHash([]any{in.TitleHash, in.TitleRevision, roleVersions})
	if e != nil {
		return nil, e
	}
	makePlan := func(kind, key string, payload map[string]any) *CandidateStagePlan {
		if payload == nil {
			payload = map[string]any{}
		}
		return &CandidateStagePlan{Type: kind, Key: fmt.Sprintf("%s:%d:%s", kind, in.CandidateID, key), CompanyID: cloneInt(in.CompanyID), AgencyID: cloneInt(in.AgencyID), CandidateID: in.CandidateID, SourceID: in.SourceID, Payload: payload}
	}
	if missing {
		return makePlan("classify_title", version, nil), nil
	}
	if !matched {
		return nil, nil
	}
	refresh := false
	if in.Snapshot != nil && in.TitleUpdatedAt != "" {
		updated, ok := stageDateMillis(in.TitleUpdatedAt)
		last, lastOK := stageDateMillis(in.Snapshot.FetchedAt)
		if in.LastSuccessfulFetch != nil && *in.LastSuccessfulFetch != 0 {
			last = *in.LastSuccessfulFetch
			lastOK = true
		}
		if !ok || !lastOK {
			return nil, ErrJobInput
		}
		refresh = updated > last
	}
	if in.Snapshot == nil || refresh {
		suffix := ""
		if in.SourceID == "rabota" {
			suffix = ":rabota-body-v3"
		}
		return makePlan("fetch_full_text", version+suffix, map[string]any{"refresh": refresh}), nil
	}
	if in.ExtractionID == "" {
		if in.Snapshot.ID == "" || in.ExtractionConfigHash == "" || in.ExtractionPromptVersion == "" {
			return nil, ErrJobInput
		}
		return makePlan("extract_vacancy", version+":"+in.Snapshot.ID+":"+in.ExtractionConfigHash+":"+in.ExtractionPromptVersion, nil), nil
	}
	if in.LeafInputHash == "" {
		return nil, ErrJobInput
	}
	if in.Leaf == nil {
		return makePlan("assign_roles", in.LeafInputHash, nil), nil
	}
	if in.Leaf.InputHash != in.LeafInputHash || in.Leaf.ID == "" {
		return nil, ErrJobInput
	}
	if len(in.Leaf.RoleIDs) == 0 {
		return nil, nil
	}
	if in.Imported {
		return nil, ErrImportedCandidateResolution
	}
	return makePlan("import_vacancy", in.LeafInputHash+":"+in.ExtractionID+":"+in.Leaf.ID+":auto-separate-v1", nil), nil
}
