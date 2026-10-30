package parser

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/aiproviders"
	"itmap/internal/roles"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrCandidateChanged = errors.New("candidate title, scope or prompts changed")
var ErrClassificationBusy = errors.New("candidate or classification slots busy")

// Owner задаёт серверный dispatcher из владельца цикла, не из payload задания.
type TitleStageStore struct{ Owner string }
type titleSnapshot struct {
	CandidateID, Revision int64
	Raw, Hash             string
	Roles                 []titleRole
	Needed                []aiproviders.TitleRole
	Token                 string
	Slot                  int
	Started               int64
	RetainSlot            bool
}
type titleRole struct {
	Role aiproviders.TitleRole
	Hash string
}

func jsStageTrim(s string) string {
	return strings.Trim(s, "\u0009\u000A\u000B\u000C\u000D\u0020\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFF")
}
func stageHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func stageToken() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", ErrSettingsStorage
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func (s TitleStageStore) read(ctx context.Context, tx *sql.Tx, j Job) (titleSnapshot, error) {
	if s.Owner == "" || j.Type != "classify_title" || j.CandidateID == nil || *j.CandidateID < 1 || j.SourceID == nil || (j.CompanyID == nil) == (j.AgencyID == nil) {
		return titleSnapshot{}, ErrJobInput
	}
	row, e := readSettingsRow(ctx, tx, true)
	if e != nil {
		return titleSnapshot{}, ErrSettingsStorage
	}
	view, e := settingsView(ctx, tx, row)
	if e != nil {
		return titleSnapshot{}, ErrSettingsStorage
	}
	var settings Settings
	if json.Unmarshal(view.PublicJSON(), &settings) != nil || !settings.Enabled {
		return titleSnapshot{}, ErrCandidateChanged
	}
	selected := false
	for _, id := range settings.SourceIDs {
		if id == *j.SourceID {
			selected = true
		}
	}
	if !selected || j.AgencyID != nil && !settings.AgenciesEnabled {
		return titleSnapshot{}, ErrCandidateChanged
	}

	table, column, id := "company_identity", "company_name", j.CompanyID
	if j.AgencyID != nil {
		table, column, id = "agency_identity", "agency_name", j.AgencyID
	}
	var name string
	if e = tx.QueryRowContext(ctx, "SELECT "+column+" FROM "+table+" WHERE id=? FOR UPDATE", id).Scan(&name); errors.Is(e, sql.ErrNoRows) {
		return titleSnapshot{}, ErrCandidateChanged
	} else if e != nil {
		return titleSnapshot{}, ErrSettingsStorage
	}
	if row.favorite != 0 {
		var favorite int
		e = tx.QueryRowContext(ctx, "SELECT favorite FROM company_user_state WHERE company_name=? AND user_name=? FOR UPDATE", name, s.Owner).Scan(&favorite)
		if errors.Is(e, sql.ErrNoRows) || e == nil && favorite != 1 {
			return titleSnapshot{}, ErrCandidateChanged
		}
		if e != nil {
			return titleSnapshot{}, ErrSettingsStorage
		}
	}

	x := titleSnapshot{CandidateID: *j.CandidateID}
	var company, agency sql.NullInt64
	var source sql.NullString
	if e = tx.QueryRowContext(ctx, "SELECT title_raw,title_hash,title_revision,company_id,agency_id,source_id FROM vacancy_candidates WHERE id=? FOR UPDATE", x.CandidateID).Scan(&x.Raw, &x.Hash, &x.Revision, &company, &agency, &source); errors.Is(e, sql.ErrNoRows) {
		return x, ErrCandidateChanged
	} else if e != nil {
		return x, ErrSettingsStorage
	}
	if !utf8.ValidString(x.Raw) || strings.ContainsRune(x.Raw, 0) || utf8.RuneCountInString(x.Raw) > 500 || jsStageTrim(x.Raw) == "" {
		return x, ErrCandidateChanged
	}
	if !reflect.DeepEqual(nullableInt(company), j.CompanyID) || !reflect.DeepEqual(nullableInt(agency), j.AgencyID) || !source.Valid || source.String != *j.SourceID || stageHash(jsStageTrim(x.Raw)) != x.Hash {
		return x, ErrCandidateChanged
	}
	rows, e := tx.QueryContext(ctx, "SELECT r.id,r.stable_key,r.name,r.sort_order,c.markdown,c.revision FROM vacancy_roles r JOIN vacancy_role_classification c ON c.role_id=r.id WHERE r.parent_id IS NULL AND c.parsing_enabled=1 ORDER BY r.sort_order,r.id FOR UPDATE")
	if e != nil {
		return x, ErrSettingsStorage
	}
	catalogRows := []roles.Role{}
	byID := map[int64]titleRole{}
	for rows.Next() {
		var r roles.Role
		var rule aiproviders.TitleRole
		if e = rows.Scan(&r.ID, &r.StableKey, &r.Name, &r.SortOrder, &rule.Markdown, &rule.Revision); e != nil {
			break
		}
		if jsStageTrim(rule.Markdown) == "" {
			continue
		}
		rule.RoleID, rule.Name = r.ID, r.Name
		catalogRows = append(catalogRows, r)
		byID[r.ID] = titleRole{Role: rule, Hash: stageHash(rule.Markdown)}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return x, ErrSettingsStorage
	}
	catalog, e := roles.Build(catalogRows)
	if e != nil {
		return x, ErrSettingsStorage
	}
	x.Roles = []titleRole{}
	for _, r := range catalog.Roots {
		x.Roles = append(x.Roles, byID[r.ID])
	}
	if len(x.Roles) == 0 {
		return x, ErrCandidateChanged
	}
	return x, nil
}
func (s TitleStageStore) Prepare(ctx context.Context, tx *sql.Tx, j Job) (AIStagePlan, error) {
	x, e := s.read(ctx, tx, j)
	if e != nil {
		return AIStagePlan{}, e
	}
	cached := aiproviders.TitleResult{Results: []aiproviders.TitleDecision{}}
	for _, role := range x.Roles {
		var decision, model string
		e = tx.QueryRowContext(ctx, "SELECT decision,model_id FROM vacancy_classifications WHERE candidate_id=? AND root_role_id=? AND title_hash=? AND title_revision=? AND prompt_version=? AND model_key=? ORDER BY id DESC LIMIT 1 FOR UPDATE", x.CandidateID, role.Role.RoleID, x.Hash, x.Revision, role.Hash, aiproviders.JEVModelID).Scan(&decision, &model)
		if errors.Is(e, sql.ErrNoRows) {
			x.Needed = append(x.Needed, role.Role)
			continue
		}
		if e != nil {
			return AIStagePlan{}, ErrSettingsStorage
		}
		if decision != "match" && decision != "reject" || model == "" {
			return AIStagePlan{}, ErrSettingsStorage
		}
		choice := decision
		cached.Model = model
		cached.Results = append(cached.Results, aiproviders.TitleDecision{RoleID: role.Role.RoleID, Name: role.Role.Name, PromptRevision: role.Role.Revision, Decision: &choice})
	}
	plan := AIStagePlan{Request: aiproviders.GenerationRequest{Task: "title_classification"}, Title: jsStageTrim(x.Raw), TitleRoles: x.Needed}
	if len(x.Needed) == 0 {
		plan.Cached = &AIStageOutput{Classification: &cached}
		plan.provenance = x
		return plan, nil
	}
	x.Token, e = stageToken()
	if e != nil {
		return AIStagePlan{}, e
	}
	x.Started, e = runClock(ctx, tx)
	if e != nil {
		return AIStagePlan{}, ErrSettingsStorage
	}
	const candidateLease = `INSERT INTO vacancy_candidate_classification_leases(candidate_id,generation,owner_token,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE owner_token=IF(generation<>VALUES(generation) OR expires_at<=?,VALUES(owner_token),owner_token),expires_at=IF(generation<>VALUES(generation) OR expires_at<=?,VALUES(expires_at),expires_at),generation=VALUES(generation)`
	if _, e = tx.ExecContext(ctx, candidateLease, x.CandidateID, j.Generation, x.Token, x.Started+300000, x.Started, x.Started); e != nil {
		return AIStagePlan{}, ErrSettingsStorage
	}
	var token string
	if tx.QueryRowContext(ctx, "SELECT owner_token FROM vacancy_candidate_classification_leases WHERE candidate_id=?", x.CandidateID).Scan(&token) != nil {
		return AIStagePlan{}, ErrSettingsStorage
	}
	if token != x.Token {
		return AIStagePlan{}, ErrClassificationBusy
	}
	for slot := 1; slot <= 2; slot++ {
		_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_classification_slots(slot,generation,owner_token,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE owner_token=IF(generation<>VALUES(generation) OR expires_at<=?,VALUES(owner_token),owner_token),expires_at=IF(generation<>VALUES(generation) OR expires_at<=?,VALUES(expires_at),expires_at),generation=VALUES(generation)`, slot, j.Generation, x.Token, x.Started+300000, x.Started, x.Started)
		if e != nil {
			return AIStagePlan{}, ErrSettingsStorage
		}
		if tx.QueryRowContext(ctx, "SELECT owner_token FROM vacancy_classification_slots WHERE slot=?", slot).Scan(&token) != nil {
			return AIStagePlan{}, ErrSettingsStorage
		}
		if token == x.Token {
			x.Slot = slot
			break
		}
	}
	if x.Slot == 0 {
		return AIStagePlan{}, ErrClassificationBusy
	}
	plan.provenance = x
	return plan, nil
}
func (s TitleStageStore) Revalidate(ctx context.Context, tx *sql.Tx, j Job, p AIStagePlan) error {
	old, ok := p.provenance.(titleSnapshot)
	if !ok {
		return ErrJobInput
	}
	now, e := s.read(ctx, tx, j)
	if e != nil {
		return e
	}
	if old.CandidateID != now.CandidateID || old.Hash != now.Hash || old.Revision != now.Revision || old.Raw != now.Raw || !reflect.DeepEqual(old.Roles, now.Roles) {
		return ErrCandidateChanged
	}
	if p.Cached != nil {
		return nil
	}
	var owned bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM vacancy_candidate_classification_leases c JOIN vacancy_classification_slots s ON s.slot=? WHERE c.candidate_id=? AND c.generation=? AND s.generation=c.generation AND c.owner_token=? AND s.owner_token=c.owner_token AND LEAST(c.expires_at,s.expires_at)>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED))`, old.Slot, old.CandidateID, j.Generation, old.Token).Scan(&owned)
	if e != nil {
		return ErrSettingsStorage
	}
	if !owned {
		return ErrClassificationBusy
	}
	return nil
}
func (s TitleStageStore) Save(ctx context.Context, tx *sql.Tx, j Job, p AIStagePlan, out AIStageOutput) error {
	x, ok := p.provenance.(titleSnapshot)
	if !ok || out.Classification == nil {
		return ErrJobInput
	}
	r := out.Classification
	if r.Model != "" && r.Model != aiproviders.JEVModelID && !strings.HasPrefix(r.Model, aiproviders.JEVModelID+"-") {
		return ErrCandidateChanged
	}
	needed := map[int64]titleRole{}
	requested := []map[string]any{}
	for _, role := range x.Roles {
		for _, n := range x.Needed {
			if n.RoleID == role.Role.RoleID {
				needed[n.RoleID] = role
				requested = append(requested, map[string]any{"roleId": n.RoleID, "name": n.Name, "promptVersion": role.Hash, "promptRevision": n.Revision})
			}
		}
	}
	if len(r.Results) != len(needed) {
		return ErrCandidateChanged
	}
	errorsOut := []map[string]any{}
	valid := []aiproviders.TitleDecision{}
	seen := map[int64]bool{}
	for _, d := range r.Results {
		role, ok := needed[d.RoleID]
		if !ok || seen[d.RoleID] || d.PromptRevision != role.Role.Revision {
			return ErrCandidateChanged
		}
		seen[d.RoleID] = true
		if d.Error == nil && d.Decision != nil && (*d.Decision == "match" || *d.Decision == "reject") {
			valid = append(valid, d)
		} else {
			code, message := "invalid_output", "JEV не вернула корректное бинарное решение для этой роли."
			if d.Error != nil {
				code = aiStageErrorCode(d.Error)
				message = d.Error.Message
			}
			errorsOut = append(errorsOut, map[string]any{"roleId": d.RoleID, "promptVersion": role.Hash, "code": code, "message": message})
		}
	}
	outcome := "complete"
	if len(errorsOut) > 0 {
		outcome = "error"
		if len(valid) > 0 {
			outcome = "partial"
		}
	}
	if len(valid) > 0 && r.Model == "" {
		return ErrCandidateChanged
	}
	id, e := stageToken()
	if e != nil {
		return e
	}
	requestedJSON, _ := json.Marshal(requested)
	errorsJSON, _ := json.Marshal(errorsOut)
	at := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_classification_attempts(id,candidate_id,title_raw,title_hash,title_revision,model_key,model_id,requested_roles_json,errors_json,outcome,classified_at,duration_ms,cost_usd) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, x.CandidateID, x.Raw, x.Hash, x.Revision, aiproviders.JEVModelID, nullStageModel(r.Model), string(requestedJSON), string(errorsJSON), outcome, at, now-x.Started, r.CostUSD)
	if e != nil {
		return ErrSettingsStorage
	}
	for _, d := range valid {
		role := needed[d.RoleID]
		_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_classifications(candidate_id,attempt_id,root_role_id,root_role_name,decision,model_key,model_id,prompt_version,prompt_revision,title_hash,title_revision,classified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, x.CandidateID, id, d.RoleID, role.Role.Name, *d.Decision, aiproviders.JEVModelID, r.Model, role.Hash, d.PromptRevision, x.Hash, x.Revision, at)
		if e != nil {
			return ErrSettingsStorage
		}
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM vacancy_candidate_classification_leases WHERE candidate_id=? AND generation=? AND owner_token=?", x.CandidateID, j.Generation, x.Token); e != nil {
		return ErrSettingsStorage
	}
	if x.RetainSlot {
		return nil
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM vacancy_classification_slots WHERE slot=? AND generation=? AND owner_token=?", x.Slot, j.Generation, x.Token); e != nil {
		return ErrSettingsStorage
	}
	return nil
}

func nullStageModel(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s TitleStageStore) SaveFailure(ctx context.Context, tx *sql.Tx, j Job, p AIStagePlan, failure error) error {
	x, ok := p.provenance.(titleSnapshot)
	if !ok {
		return ErrJobInput
	}
	code := aiStageErrorCode(failure)
	x.RetainSlot = code == "network" || code == "timeout" || code == "cancelled"
	p.provenance = x
	r := aiproviders.TitleResult{Results: []aiproviders.TitleDecision{}}
	for _, role := range x.Needed {
		r.Results = append(r.Results, aiproviders.TitleDecision{RoleID: role.RoleID, PromptRevision: role.Revision, Error: &aiproviders.ServiceError{Code: code, Status: 502, Message: "Не удалось выполнить классификацию."}})
	}
	return s.Save(ctx, tx, j, p, AIStageOutput{Classification: &r})
}

// Снимает только lease, полученные этим plan, если attempt не удалось начать.
func (s TitleStageStore) Abort(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	x, ok := plan.provenance.(titleSnapshot)
	if !ok {
		return ErrJobInput
	}
	if plan.Cached != nil {
		return nil
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM vacancy_candidate_classification_leases WHERE candidate_id=? AND generation=? AND owner_token=?", x.CandidateID, j.Generation, x.Token); e != nil {
		return ErrSettingsStorage
	}
	if _, e := tx.ExecContext(ctx, "DELETE FROM vacancy_classification_slots WHERE slot=? AND generation=? AND owner_token=?", x.Slot, j.Generation, x.Token); e != nil {
		return ErrSettingsStorage
	}
	return nil
}
