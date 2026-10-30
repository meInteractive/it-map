package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"itmap/internal/aiproviders"
	"itmap/internal/backup"
	"itmap/internal/profiles"
	"math"
	"net"
	"reflect"
	"regexp"
	"strings"
	"time"
)

var ErrDetailBusy = errors.New("vacancy detail operation or LLM slots busy")
var ErrExtractionChanged = errors.New("vacancy extraction dependencies changed")
var ErrExtractionCacheAmbiguous = errors.New("latest extraction cache is ambiguous")

type ExtractionStageStore struct{ Owner string }
type extractionSnapshot struct {
	Title                          titleSnapshot
	Snapshot                       StoredRawSnapshot
	Source                         ExtractionSource
	Prompt, PromptHash, ConfigHash string
	Config                         profiles.ModelInput
	Token, Slot                    string
	Started                        int64
}

var hostLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var numericHost = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){3}$`)

func validExtractionURL(value string) bool {
	if strings.IndexFunc(value, func(r rune) bool { return r < 32 }) >= 0 {
		return false
	}
	u, e := whatwg.Parse(value)
	if e != nil || (u.Scheme() != "http" && u.Scheme() != "https") || u.Username() != "" || u.Password() != "" || u.Port() != "" || utf16Length(u.Href(false)) > 2048 {
		return false
	}
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if len(h) > 253 || !strings.Contains(h, ".") || net.ParseIP(h) != nil || numericHost.MatchString(h) {
		return false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if !hostLabel.MatchString(l) {
			return false
		}
	}
	switch labels[len(labels)-1] {
	case "localhost", "local", "internal", "lan", "test", "invalid", "example", "onion":
		return false
	}
	return h != "metadata.google.internal"
}

func (s ExtractionStageStore) read(ctx context.Context, tx *sql.Tx, j Job) (extractionSnapshot, error) {
	if j.Type != "extract_vacancy" {
		return extractionSnapshot{}, ErrJobInput
	}
	shadow := j
	shadow.Type = "classify_title"
	title, e := (TitleStageStore{Owner: s.Owner}).read(ctx, tx, shadow)
	if e != nil {
		return extractionSnapshot{}, e
	}
	matched := false
	for _, role := range title.Roles {
		var decision string
		e = tx.QueryRowContext(ctx, `SELECT decision FROM vacancy_classifications WHERE candidate_id=? AND root_role_id=? AND title_hash=? AND title_revision=? AND prompt_version=? AND model_key=? ORDER BY id DESC LIMIT 1 FOR UPDATE`, title.CandidateID, role.Role.RoleID, title.Hash, title.Revision, role.Hash, aiproviders.JEVModelID).Scan(&decision)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return extractionSnapshot{}, ErrSettingsStorage
		}
		if decision == "match" {
			matched = true
		}
	}
	if !matched {
		return extractionSnapshot{}, ErrCandidateChanged
	}
	x := extractionSnapshot{Title: title}
	var vacancyURL sql.NullString
	var companyRaw string
	if e = tx.QueryRowContext(ctx, "SELECT vacancy_url,company_name_raw FROM vacancy_candidates WHERE id=? FOR UPDATE", title.CandidateID).Scan(&vacancyURL, &companyRaw); e != nil {
		return x, ErrSettingsStorage
	}
	if !vacancyURL.Valid || !validExtractionURL(vacancyURL.String) {
		return x, ErrCandidateChanged
	}
	url := vacancyURL.String
	var company string
	if j.CompanyID != nil {
		e = tx.QueryRowContext(ctx, "SELECT company_name FROM company_identity WHERE id=? FOR UPDATE", j.CompanyID).Scan(&company)
	} else {
		e = tx.QueryRowContext(ctx, "SELECT agency_name FROM agency_identity WHERE id=? FOR UPDATE", j.AgencyID).Scan(&company)
	}
	if e != nil {
		return x, ErrSettingsStorage
	}
	if company == "" {
		company = companyRaw
	}
	x.Source = ExtractionSource{Title: title.Raw, Company: company, URL: &url}
	x.Snapshot, e = ReadRawSnapshot(ctx, tx, title.CandidateID, url, *j.SourceID)
	if e != nil {
		return x, e
	}
	p := profiles.DefaultPrompts().Prompts["vacancy_extraction"]
	m := profiles.DefaultModels()["vacancy_extraction"]
	x.Prompt = p.Markdown
	x.Config = profiles.ModelInput{Provider: m.Provider, OpenAIModel: m.OpenAIModel, OpenRouterModel: "best_free", ReasoningEffort: m.ReasoningEffort, Revision: m.Revision}
	var promptRev, modelRev int64
	e = tx.QueryRowContext(ctx, `SELECT markdown,prompt_revision,provider,openai_model,reasoning_effort,model_revision FROM vacancy_detail_settings WHERE id=1 FOR UPDATE`).Scan(&x.Prompt, &promptRev, &x.Config.Provider, &x.Config.OpenAIModel, &x.Config.ReasoningEffort, &modelRev)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return x, ErrSettingsStorage
	}
	if e == nil {
		x.Config.Revision = modelRev
	}
	if e = profiles.ValidateModel("vacancy_extraction", x.Config); e != nil {
		return x, e
	}
	x.PromptHash = stageHash(x.Prompt)
	configBytes, e := json.Marshal([]string{x.Config.Provider, x.Config.OpenAIModel, x.Config.ReasoningEffort, x.Config.OpenRouterModel})
	if e != nil {
		return x, ErrSettingsStorage
	}
	configBytes, e = backup.StringifyParsedJSON(configBytes)
	if e != nil {
		return x, ErrSettingsStorage
	}
	x.ConfigHash = stageHash(string(configBytes))
	return x, nil
}

type extractionCache struct{ ID, Data, Model, At string }

func readExtractionCache(ctx context.Context, tx *sql.Tx, x extractionSnapshot) (*extractionCache, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,extracted_data,model_id,processed_at FROM vacancy_extractions WHERE candidate_id=? AND snapshot_id=? AND config_hash=? AND prompt_version=? AND schema_version=? AND status='success' ORDER BY processed_at DESC,id DESC LIMIT 2 FOR UPDATE`, x.Title.CandidateID, x.Snapshot.ID, x.ConfigHash, x.PromptHash, ExtractionSchemaVersion)
	if e != nil {
		return nil, ErrSettingsStorage
	}
	found := []extractionCache{}
	for rows.Next() {
		var c extractionCache
		var data, model sql.NullString
		if e = rows.Scan(&c.ID, &data, &model, &c.At); e != nil {
			break
		}
		if !data.Valid || !model.Valid {
			rows.Close()
			return nil, ErrSnapshotInvalid
		}
		c.Data, c.Model = data.String, model.String
		found = append(found, c)
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
		return nil, ErrExtractionCacheAmbiguous
	}
	normalized, e := NormalizeExtraction(found[0].Data, x.Snapshot.Markdown)
	if e != nil || len(normalized.Corrections) > 0 {
		return nil, ErrSnapshotInvalid
	}
	return &found[0], nil
}
func (s ExtractionStageStore) Prepare(ctx context.Context, tx *sql.Tx, j Job) (AIStagePlan, error) {
	x, e := s.read(ctx, tx, j)
	if e != nil {
		return AIStagePlan{}, e
	}
	cached, e := readExtractionCache(ctx, tx, x)
	if e != nil {
		return AIStagePlan{}, e
	}
	plan := AIStagePlan{Request: aiproviders.GenerationRequest{Task: "vacancy_extraction"}, Config: x.Config}
	if cached != nil {
		plan.Cached = &AIStageOutput{Generation: aiproviders.AIResult{Model: cached.Model, Message: aiproviders.Message{Content: cached.Data}}}
		plan.provenance = struct {
			Snapshot extractionSnapshot
			Cached   *extractionCache
		}{x, cached}
		return plan, nil
	}
	request, requestError := PrepareExtractionRequest(x.Prompt, x.Snapshot.Markdown, x.Source)
	if requestError == nil {
		plan.Request = request
	} else {
		var typed *aiproviders.ServiceError
		if !errors.As(requestError, &typed) || (typed.Code != "input_too_large" && typed.Code != "empty_prompt") {
			return AIStagePlan{}, requestError
		}
		plan.PreflightError = requestError
	}
	x.Token, e = stageToken()
	if e != nil {
		return AIStagePlan{}, e
	}
	x.Started, e = runClock(ctx, tx)
	if e != nil {
		return AIStagePlan{}, ErrSettingsStorage
	}
	acquire := func(key string, candidate bool) (bool, error) {
		condition := "expires_at<=?"
		if candidate {
			condition = "generation<>VALUES(generation) OR expires_at<=?"
		}
		q := `INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE owner_token=IF(` + condition + `,VALUES(owner_token),owner_token),expires_at=IF(` + condition + `,VALUES(expires_at),expires_at),generation=IF(owner_token=VALUES(owner_token),VALUES(generation),generation)`
		if _, e := tx.ExecContext(ctx, q, key, j.Generation, x.Token, x.Started+300000, x.Started, x.Started); e != nil {
			return false, ErrSettingsStorage
		}
		var token string
		if e := tx.QueryRowContext(ctx, "SELECT owner_token FROM vacancy_detail_leases WHERE lease_key=?", key).Scan(&token); e != nil {
			return false, ErrSettingsStorage
		}
		return token == x.Token, nil
	}
	ok, e := acquire(fmt.Sprintf("candidate:%d", x.Title.CandidateID), true)
	if e != nil {
		return AIStagePlan{}, e
	}
	if !ok {
		return AIStagePlan{}, ErrDetailBusy
	}
	if plan.PreflightError != nil {
		plan.provenance = struct {
			Snapshot extractionSnapshot
			Cached   *extractionCache
		}{x, nil}
		return plan, nil
	}
	for _, key := range []string{"llm:1", "llm:2"} {
		ok, e = acquire(key, false)
		if e != nil {
			return AIStagePlan{}, e
		}
		if ok {
			x.Slot = key
			break
		}
	}
	if x.Slot == "" {
		return AIStagePlan{}, ErrDetailBusy
	}
	plan.provenance = struct {
		Snapshot extractionSnapshot
		Cached   *extractionCache
	}{x, nil}
	return plan, nil
}
func (s ExtractionStageStore) Revalidate(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	p, ok := plan.provenance.(struct {
		Snapshot extractionSnapshot
		Cached   *extractionCache
	})
	if !ok {
		return ErrJobInput
	}
	old := p.Snapshot
	now, e := s.read(ctx, tx, j)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(old.Title, now.Title) || !reflect.DeepEqual(old.Snapshot, now.Snapshot) || !reflect.DeepEqual(old.Source, now.Source) || old.PromptHash != now.PromptHash || old.ConfigHash != now.ConfigHash || old.Config.Revision != now.Config.Revision {
		return ErrExtractionChanged
	}
	if p.Cached != nil {
		c, e := readExtractionCache(ctx, tx, now)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(c, p.Cached) {
			return ErrExtractionChanged
		}
		return nil
	}
	rows, e := tx.QueryContext(ctx, `SELECT lease_key FROM vacancy_detail_leases WHERE lease_key IN (?,?) AND generation=? AND owner_token=? AND expires_at>CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED) FOR UPDATE`, fmt.Sprintf("candidate:%d", old.Title.CandidateID), old.Slot, j.Generation, old.Token)
	if e != nil {
		return ErrSettingsStorage
	}
	n := 0
	for rows.Next() {
		var key string
		if e = rows.Scan(&key); e != nil {
			break
		}
		n++
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return ErrSettingsStorage
	}
	expected := 2
	if old.Slot == "" && plan.PreflightError != nil {
		expected = 1
	}
	if n != expected {
		return ErrDetailBusy
	}
	return nil
}
func extractionUsage(raw json.RawMessage, provider, model string) (any, any, any, bool) {
	var usage map[string]json.RawMessage
	_ = json.Unmarshal(raw, &usage)
	get := func(keys ...string) (float64, bool) {
		for _, key := range keys {
			if v := usage[key]; len(v) > 0 && string(v) != "null" {
				return jsonNumber(v)
			}
		}
		return 0, false
	}
	input, hasInput := get("input_tokens", "prompt_tokens")
	output, hasOutput := get("output_tokens", "completion_tokens")
	cost, hasCost := get("cost")
	var in, out, value any
	if hasInput && math.Trunc(input) == input && input <= 9007199254740991 {
		in = int64(input)
	}
	if hasOutput && math.Trunc(output) == output && output <= 9007199254740991 {
		out = int64(output)
	}
	estimated := false
	if !hasCost && provider == "openai" && hasInput && hasOutput {
		var details map[string]json.RawMessage
		_ = json.Unmarshal(usage["input_tokens_details"], &details)
		cachedRaw := details["cached_tokens"]
		cached, cachedKnown := jsonNumber(cachedRaw)
		safeCache := len(cachedRaw) == 0 || string(cachedRaw) == "null" || cachedKnown && cached <= 0
		if rate, ok := historicalPrices[model]; ok && safeCache {
			cost = (input*rate.Input + output*rate.Output) / 1e6
			hasCost = finiteMoney(cost)
			estimated = hasCost
		}
	}
	if hasCost {
		value = cost
	}
	return in, out, value, estimated
}
func (s ExtractionStageStore) Save(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan, out AIStageOutput) error {
	p, ok := plan.provenance.(struct {
		Snapshot extractionSnapshot
		Cached   *extractionCache
	})
	if !ok || p.Cached != nil {
		return ErrJobInput
	}
	x := p.Snapshot
	if out.Generation.Model == "" {
		return ErrJobInput
	}
	result, e := NormalizeExtraction(out.Generation.Message.Content, x.Snapshot.Markdown)
	if e != nil {
		return e
	}
	data, e := json.Marshal(result.Data)
	if e != nil {
		return ErrSettingsStorage
	}
	data, e = backup.StringifyParsedJSON(data)
	if e != nil {
		return ErrSettingsStorage
	}
	in, output, cost, estimated := extractionUsage(out.Generation.Usage, x.Config.Provider, out.Generation.Model)
	employer, _ := result.Data["employer_name"].(string)
	sourceCompany := x.Source.Company
	lower := cases.Lower(language.Und)
	diagnostics := map[string]any{"sanitizedFields": result.Corrections, "costEstimated": estimated, "sourceCompany": sourceCompany, "extractedEmployer": result.Data["employer_name"], "employerMismatch": sourceCompany != "" && employer != "" && lower.String(sourceCompany) != lower.String(employer), "sourceUrl": x.Source.URL}
	diag, e := json.Marshal(diagnostics)
	if e != nil {
		return ErrSettingsStorage
	}
	id, e := stageToken()
	if e != nil {
		return e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	at := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,input_tokens,output_tokens,cost_usd,status,diagnostics) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'success',?)`, id, x.Title.CandidateID, x.Snapshot.ID, x.Config.Provider, out.Generation.Model, x.ConfigHash, x.PromptHash, ExtractionSchemaVersion, string(data), at, now-x.Started, in, output, cost, string(diag))
	if e != nil {
		return ErrSettingsStorage
	}
	return releaseDetail(ctx, tx, j, x, false)
}
func releaseDetail(ctx context.Context, tx *sql.Tx, j Job, x extractionSnapshot, retain bool) error {
	if retain {
		return nil
	}
	_, e := tx.ExecContext(ctx, "DELETE FROM vacancy_detail_leases WHERE lease_key IN (?,?) AND generation=? AND owner_token=?", fmt.Sprintf("candidate:%d", x.Title.CandidateID), x.Slot, j.Generation, x.Token)
	if e != nil {
		return ErrSettingsStorage
	}
	return nil
}
func extractionFailureMessage(failure error) string {
	var typed *aiproviders.ServiceError
	if errors.As(failure, &typed) {
		if typed.Code == "input_too_large" && typed.Status == 422 && typed.Message == "Исходный Markdown сохранён целиком, но превышает безопасный лимит 120 000 символов для запроса LLM. Структурирование ожидает обработки без усечения." {
			return typed.Message
		}
		if typed.Code == "empty_prompt" && typed.Status == 409 && typed.Message == "Заполните промт «Структурирование вакансии»." {
			return typed.Message
		}
	}
	if errors.As(failure, &typed) && typed.Code == "invalid_output" && typed.Status == 422 {
		switch typed.Message {
		case "Модель вернула некорректный JSON.", "Поля ответа не соответствуют схеме.", "Недостаточное содержательное описание.", "Границы зарплаты перепутаны.", "Зарплата не подтверждается исходным текстом.", "Дата публикации должна быть действительной датой YYYY-MM-DD.":
			return typed.Message
		}
		var schema struct{ Required []string }
		_ = json.Unmarshal(ExtractionSchema(), &schema)
		for _, key := range schema.Required {
			if typed.Message == "Некорректное поле "+key+"." {
				return typed.Message
			}
		}
	}
	return "Запрос прерван; результат и стоимость неизвестны."
}

func (s ExtractionStageStore) SaveFailure(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan, failure error) error {
	p, ok := plan.provenance.(struct {
		Snapshot extractionSnapshot
		Cached   *extractionCache
	})
	if !ok || p.Cached != nil {
		return ErrJobInput
	}
	x := p.Snapshot
	code := aiStageErrorCode(failure)
	id, e := stageToken()
	if e != nil {
		return e
	}
	now, e := runClock(ctx, tx)
	if e != nil {
		return ErrSettingsStorage
	}
	diag, _ := json.Marshal(map[string]any{"message": extractionFailureMessage(failure), "costEstimated": false})
	_, e = tx.ExecContext(ctx, `INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,processed_at,duration_ms,status,error_code,diagnostics) VALUES(?,?,?,?,NULL,?,?,?,?,?,'error',?,?)`, id, x.Title.CandidateID, x.Snapshot.ID, x.Config.Provider, x.ConfigHash, x.PromptHash, ExtractionSchemaVersion, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), now-x.Started, code, string(diag))
	if e != nil {
		return ErrSettingsStorage
	}
	return releaseDetail(ctx, tx, j, x, code == "network" || code == "timeout" || code == "cancelled")
}

func (s ExtractionStageStore) Abort(ctx context.Context, tx *sql.Tx, j Job, plan AIStagePlan) error {
	p, ok := plan.provenance.(struct {
		Snapshot extractionSnapshot
		Cached   *extractionCache
	})
	if !ok {
		return ErrJobInput
	}
	if p.Cached != nil {
		return nil
	}
	return releaseDetail(ctx, tx, j, p.Snapshot, false)
}
