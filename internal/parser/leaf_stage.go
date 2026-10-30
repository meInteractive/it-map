package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/backup"
	"itmap/internal/profiles"
	"itmap/internal/roles"
	"reflect"
	"sort"
	"time"
)

var ErrExtractionMissing = errors.New("current vacancy extraction not found")
var ErrLeafChanged = errors.New("vacancy role dependencies changed")
var ErrLeafCacheAmbiguous = errors.New("latest leaf role result is ambiguous")

type LeafStageStore struct{ Owner string }
type leafAllowed struct {
	ID        int64  `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	StableKey string `json:"stableKey"`
	ParentID  *int64 `json:"parentId"`
}
type leafSnapshot struct {
	Extraction                                          extractionSnapshot
	ExtractCache                                        *extractionCache
	Allowed                                             []leafAllowed
	Matches                                             [][]any
	Prompt, PromptHash, ConfigHash, TreeHash, InputHash string
	Config                                              profiles.ModelInput
	Token, Slot                                         string
	Started                                             int64
}
type leafCache struct {
	ID, Model, At string
	IDs           []int64
}
type leafPlan struct {
	Snapshot leafSnapshot
	Cached   *leafCache
}

func canonicalHash(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", ErrSettingsStorage
	}
	b, e = backup.StringifyParsedJSON(b)
	if e != nil {
		return "", ErrSettingsStorage
	}
	return stageHash(string(b)), nil
}
func leafRoleCatalog(ctx context.Context, tx *sql.Tx) (roles.Catalog, error) {
	rows, e := tx.QueryContext(ctx, `SELECT r.id,r.stable_key,r.name,r.parent_id,r.sort_order,(SELECT COUNT(*) FROM vacancy_roles child WHERE child.parent_id=r.id) FROM vacancy_roles r ORDER BY (r.parent_id IS NOT NULL),r.parent_id,r.sort_order,r.id FOR UPDATE`)
	if e != nil {
		return roles.Catalog{}, ErrSettingsStorage
	}
	list := []roles.Role{}
	for rows.Next() {
		var r roles.Role
		var parent sql.NullInt64
		if e = rows.Scan(&r.ID, &r.StableKey, &r.Name, &parent, &r.SortOrder, &r.ChildCount); e != nil {
			break
		}
		if parent.Valid {
			r.ParentID = &parent.Int64
		}
		list = append(list, r)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return roles.Catalog{}, ErrSettingsStorage
	}
	catalog, e := roles.Build(list)
	if e != nil {
		return roles.Catalog{}, ErrSettingsStorage
	}
	return catalog, nil
}
func (s LeafStageStore) read(ctx context.Context, tx *sql.Tx, j Job) (leafSnapshot, error) {
	if j.Type != "assign_roles" {
		return leafSnapshot{}, ErrJobInput
	}
	shadow := j
	shadow.Type = "extract_vacancy"
	x, e := (ExtractionStageStore{Owner: s.Owner}).read(ctx, tx, shadow)
	if e != nil {
		return leafSnapshot{}, e
	}
	extraction, e := readExtractionCache(ctx, tx, x)
	if e != nil {
		return leafSnapshot{}, e
	}
	if extraction == nil {
		return leafSnapshot{}, ErrExtractionMissing
	}
	l := leafSnapshot{Extraction: x, ExtractCache: extraction, Matches: [][]any{}, Allowed: []leafAllowed{}}
	roots := map[int64]bool{}
	for _, role := range x.Title.Roles {
		var decision string
		e = tx.QueryRowContext(ctx, `SELECT decision FROM vacancy_classifications WHERE candidate_id=? AND root_role_id=? AND title_hash=? AND title_revision=? AND prompt_version=? AND model_key=? ORDER BY id DESC LIMIT 1 FOR UPDATE`, x.Title.CandidateID, role.Role.RoleID, x.Title.Hash, x.Title.Revision, role.Hash, aiproviders.JEVModelID).Scan(&decision)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return l, ErrSettingsStorage
		}
		if decision == "match" {
			roots[role.Role.RoleID] = true
			l.Matches = append(l.Matches, []any{role.Role.RoleID, role.Hash})
		}
	}
	catalog, e := leafRoleCatalog(ctx, tx)
	if e != nil {
		return l, e
	}
	permitted := map[int64]bool{}
	for _, r := range catalog.Roots {
		if roots[r.ID] {
			for _, id := range r.DescendantIDs {
				permitted[id] = true
			}
		}
	}
	for _, r := range catalog.Leaves {
		if r.ParentID != nil && permitted[r.ID] {
			l.Allowed = append(l.Allowed, leafAllowed{r.ID, r.Path, r.Name, r.StableKey, r.ParentID})
		}
	}
	sort.Slice(l.Allowed, func(i, j int) bool { return l.Allowed[i].ID < l.Allowed[j].ID })
	l.TreeHash, e = canonicalHash(l.Allowed)
	if e != nil {
		return l, e
	}
	p := profiles.DefaultPrompts().Prompts["vacancy_role"]
	m := profiles.DefaultModels()["vacancy_role"]
	l.Prompt = p.Markdown
	l.Config = profiles.ModelInput{Provider: m.Provider, OpenAIModel: m.OpenAIModel, OpenRouterModel: m.OpenRouterModel, ReasoningEffort: m.ReasoningEffort, Revision: m.Revision}
	var promptRev, modelRev int64
	e = tx.QueryRowContext(ctx, `SELECT markdown,prompt_revision,provider,openai_model,openrouter_model,reasoning_effort,model_revision FROM vacancy_role_settings WHERE id=1 FOR UPDATE`).Scan(&l.Prompt, &promptRev, &l.Config.Provider, &l.Config.OpenAIModel, &l.Config.OpenRouterModel, &l.Config.ReasoningEffort, &modelRev)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return l, ErrSettingsStorage
	}
	if e == nil {
		l.Config.Revision = modelRev
	}
	if e = profiles.ValidateModel("vacancy_role", l.Config); e != nil {
		return l, e
	}
	l.PromptHash = stageHash(l.Prompt)
	l.ConfigHash, e = canonicalHash([]string{l.Config.Provider, l.Config.OpenAIModel, l.Config.OpenRouterModel, l.Config.ReasoningEffort})
	if e != nil {
		return l, e
	}
	mode := 1
	if l.Config.Provider == "openrouter" && l.Config.OpenRouterModel == "jev" {
		mode = 2
	}
	l.InputHash, e = canonicalHash([]any{mode, x.Title.CandidateID, x.Title.Hash, x.Title.Revision, x.Snapshot.ID, x.Snapshot.ContentHash, l.TreeHash, l.Matches, l.PromptHash, l.ConfigHash})
	return l, e
}
func readLeafCache(ctx context.Context, tx *sql.Tx, l leafSnapshot) (*leafCache, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,role_ids,model_id,processed_at FROM vacancy_role_results WHERE candidate_id=? AND input_hash=? AND status='success' ORDER BY processed_at DESC,id DESC LIMIT 2 FOR UPDATE`, l.Extraction.Title.CandidateID, l.InputHash)
	if e != nil {
		return nil, ErrSettingsStorage
	}
	type row struct {
		ID, At     string
		IDs, Model sql.NullString
	}
	found := []row{}
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.ID, &r.IDs, &r.Model, &r.At); e != nil {
			break
		}
		found = append(found, r)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return nil, ErrSettingsStorage
	}
	if len(found) == 0 {
		return nil, nil
	}
	if len(found) > 1 && found[0].At == found[1].At {
		return nil, ErrLeafCacheAmbiguous
	}
	v := found[0]
	if !v.IDs.Valid || !v.Model.Valid {
		return nil, ErrSnapshotInvalid
	}
	allowed := []aiproviders.LeafRole{}
	for _, r := range l.Allowed {
		allowed = append(allowed, aiproviders.LeafRole{ID: r.ID, Path: r.Path})
	}
	ids, e := ValidateLeafRoleIDs(`{"role_ids":`+v.IDs.String+`}`, allowed)
	if e != nil {
		return nil, ErrSnapshotInvalid
	}
	return &leafCache{v.ID, v.Model.String, v.At, ids}, nil
}
func (s LeafStageStore) Prepare(ctx context.Context, tx *sql.Tx, j Job) (AIStagePlan, error) {
	l, e := s.read(ctx, tx, j)
	if e != nil {
		return AIStagePlan{}, e
	}
	cache, e := readLeafCache(ctx, tx, l)
	if e != nil {
		return AIStagePlan{}, e
	}
	plan := AIStagePlan{Request: aiproviders.GenerationRequest{Task: "vacancy_role"}, Config: l.Config}
	if cache != nil {
		content, _ := json.Marshal(map[string]any{"role_ids": cache.IDs})
		plan.Cached = &AIStageOutput{Generation: aiproviders.AIResult{Model: cache.Model, Message: aiproviders.Message{Content: string(content)}}}
		plan.provenance = leafPlan{l, cache}
		return plan, nil
	}
	allowed := []aiproviders.LeafRole{}
	for _, r := range l.Allowed {
		allowed = append(allowed, aiproviders.LeafRole{ID: r.ID, Path: r.Path})
	}
	plan.Request, e = PrepareLeafRoleRequest(l.Prompt, l.Extraction.Title.Raw, l.Extraction.Snapshot.Markdown, allowed, l.Config)
	if e != nil {
		return AIStagePlan{}, e
	}
	l.Token, l.Slot, l.Started, e = acquireDetailLeases(ctx, tx, j, l.Extraction.Title.CandidateID, true)
	if e != nil {
		return AIStagePlan{}, e
	}
	plan.provenance = leafPlan{l, nil}
	return plan, nil
}
func (s LeafStageStore) Revalidate(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	p, ok := plan.provenance.(leafPlan)
	if !ok {
		return ErrJobInput
	}
	old := p.Snapshot
	now, e := s.read(ctx, tx, j)
	if e != nil {
		return e
	}
	if old.InputHash != now.InputHash || old.Config.Revision != now.Config.Revision || !reflect.DeepEqual(old.Extraction.Snapshot, now.Extraction.Snapshot) || !reflect.DeepEqual(old.ExtractCache, now.ExtractCache) || !reflect.DeepEqual(old.Extraction.Title, now.Extraction.Title) || !reflect.DeepEqual(old.Allowed, now.Allowed) {
		return ErrLeafChanged
	}
	if p.Cached != nil {
		cache, e := readLeafCache(ctx, tx, now)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(cache, p.Cached) {
			return ErrLeafChanged
		}
		return nil
	}
	return validateDetailLeases(ctx, tx, j, old.Extraction.Title.CandidateID, old.Slot, old.Token, false)
}
func (s LeafStageStore) Save(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan, out AIStageOutput) error {
	p, ok := plan.provenance.(leafPlan)
	if !ok || p.Cached != nil || out.Generation.Model == "" {
		return ErrJobInput
	}
	l := p.Snapshot
	allowed := []aiproviders.LeafRole{}
	for _, r := range l.Allowed {
		allowed = append(allowed, aiproviders.LeafRole{ID: r.ID, Path: r.Path})
	}
	ids, e := ValidateLeafRoleIDs(out.Generation.Message.Content, allowed)
	if e != nil {
		return e
	}
	roleIDs, _ := json.Marshal(ids)
	input, output, cost, _ := extractionUsage(out.Generation.Usage, l.Config.Provider, out.Generation.Model)
	id, e := stageToken()
	if e != nil {
		return e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,model_id,provider,role_ids,status,processed_at,duration_ms,input_tokens,output_tokens,cost_usd) VALUES(?,?,?,?,?,?,?,?,?,?,'success',?,?,?,?,?)`, id, l.Extraction.Title.CandidateID, l.Extraction.Snapshot.ID, l.InputHash, l.TreeHash, l.PromptHash, l.ConfigHash, out.Generation.Model, l.Config.Provider, string(roleIDs), time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), now-l.Started, input, output, cost)
	if e != nil {
		return ErrSettingsStorage
	}
	return releaseOwnedDetail(ctx, tx, j, l.Extraction.Title.CandidateID, l.Slot, l.Token, false)
}
func leafFailureMessage(failure error) string {
	var v *aiproviders.ServiceError
	if errors.As(failure, &v) {
		switch v.Message {
		case "Ответ модели не является JSON.", "Модель должна выбрать только разрешённые конечные ID (не более12).", "JEV не вернула решения для точного набора конечных ролей.", "JEV вернула некорректное бинарное решение конечной роли.", "JEV выбрала более 12 конечных ролей.", "Роли JEV не прошли проверку.", "Недостаточно средств на балансе OpenRouter для JEV.":
			return v.Message
		}
	}
	return "Запрос прерван; стоимость неизвестна."
}
func (s LeafStageStore) SaveFailure(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan, failure error) error {
	p, ok := plan.provenance.(leafPlan)
	if !ok || p.Cached != nil {
		return ErrJobInput
	}
	l := p.Snapshot
	id, e := stageToken()
	if e != nil {
		return e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	code := aiStageErrorCode(failure)
	_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_role_results(id,candidate_id,snapshot_id,input_hash,tree_hash,prompt_version,config_hash,model_id,provider,status,error_code,message,processed_at,duration_ms) VALUES(?,?,?,?,?,?,?,NULL,?,'error',?,?,?,?)`, id, l.Extraction.Title.CandidateID, l.Extraction.Snapshot.ID, l.InputHash, l.TreeHash, l.PromptHash, l.ConfigHash, l.Config.Provider, code, leafFailureMessage(failure), time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), now-l.Started)
	if e != nil {
		return ErrSettingsStorage
	}
	return releaseOwnedDetail(ctx, tx, j, l.Extraction.Title.CandidateID, l.Slot, l.Token, code == "network" || code == "timeout" || code == "cancelled")
}
func (s LeafStageStore) Abort(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	p, ok := plan.provenance.(leafPlan)
	if !ok {
		return ErrJobInput
	}
	if p.Cached != nil {
		return nil
	}
	l := p.Snapshot
	return releaseOwnedDetail(ctx, tx, j, l.Extraction.Title.CandidateID, l.Slot, l.Token, false)
}
