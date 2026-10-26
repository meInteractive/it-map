package profiles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func settingKinds() []string {
	return []string{"cover_letter", "relevance", "vacancy_extraction", "vacancy_role"}
}
func updatedPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	s := value.String
	return &s
}
func readPromptRow(ctx context.Context, q profileRowQueryer, kind string) (Prompt, error) {
	binding, err := PromptBinding(kind)
	if err != nil {
		return Prompt{}, err
	}
	row := Prompt{Kind: kind}
	var updated sql.NullString
	query := fmt.Sprintf(`SELECT markdown,%s,updated_at FROM %s WHERE %s=?`, binding.RevisionColumn, binding.Table, binding.KeyColumn)
	if err = q.QueryRowContext(ctx, query, binding.KeyValue).Scan(&row.Markdown, &row.Revision, &updated); err != nil {
		return Prompt{}, err
	}
	row.UpdatedAt = updatedPointer(updated)
	return row, nil
}
func readModelRow(ctx context.Context, q profileRowQueryer, kind string) (Model, error) {
	binding, err := ModelBinding(kind)
	if err != nil {
		return Model{}, err
	}
	row := Model{Kind: kind}
	var updated sql.NullString
	router := "openrouter_model"
	if kind == "vacancy_extraction" {
		router = "'best_free'"
	}
	query := fmt.Sprintf(`SELECT provider,openai_model,%s,reasoning_effort,%s,updated_at FROM %s WHERE %s=?`, router, binding.RevisionColumn, binding.Table, binding.KeyColumn)
	if err = q.QueryRowContext(ctx, query, binding.KeyValue).Scan(&row.Provider, &row.OpenAIModel, &row.OpenRouterModel, &row.ReasoningEffort, &row.Revision, &updated); err != nil {
		return Model{}, err
	}
	row.UpdatedAt = updatedPointer(updated)
	return row, nil
}

// Read-only repeatable snapshots preserve stored rows verbatim. Legacy lazy
// DDL and one-off historical prompt alignments are not run from Go GET paths.
func (r Repository) ReadPrompts(ctx context.Context) (PromptCollection, error) {
	if r.DB == nil {
		return PromptCollection{}, ErrUnavailable
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return PromptCollection{}, err
	}
	defer tx.Rollback()
	result := DefaultPrompts()
	for _, kind := range settingKinds() {
		row, e := readPromptRow(ctx, tx, kind)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return PromptCollection{}, e
		}
		result.Prompts[kind] = row
	}
	if err = tx.Commit(); err != nil {
		return PromptCollection{}, err
	}
	return result, nil
}
func (r Repository) ReadModels(ctx context.Context) (map[string]Model, error) {
	if r.DB == nil {
		return nil, ErrUnavailable
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := DefaultModels()
	for _, kind := range settingKinds() {
		row, e := readModelRow(ctx, tx, kind)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return nil, e
		}
		result[kind] = row
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
func bootstrapSetting(ctx context.Context, tx *sql.Tx, kind string, model bool) (Binding, error) {
	var binding Binding
	var err error
	if model {
		binding, err = ModelBinding(kind)
	} else {
		binding, err = PromptBinding(kind)
	}
	if err != nil {
		return Binding{}, err
	}
	// Table/column identifiers come only from the exact kind allowlist. No-op
	// UPSERT acquires X immediately; all default initialization is transactional.
	switch kind {
	case "cover_letter", "relevance":
		query := fmt.Sprintf(`INSERT INTO %s(kind) VALUES(?) ON DUPLICATE KEY UPDATE kind=VALUES(kind)`, binding.Table)
		_, err = tx.ExecContext(ctx, query, kind)
	case "vacancy_extraction":
		_, err = tx.ExecContext(ctx, `INSERT INTO vacancy_detail_settings(id,markdown) VALUES(1,?) ON DUPLICATE KEY UPDATE id=VALUES(id)`, DefaultPrompts().Defaults[kind])
	case "vacancy_role":
		_, err = tx.ExecContext(ctx, `INSERT INTO vacancy_role_settings(id,markdown,provider,openrouter_model) VALUES(1,?,'openrouter','jev') ON DUPLICATE KEY UPDATE id=VALUES(id)`, DefaultPrompts().Defaults[kind])
	}
	// Preserve actual legacy INSERT defaults, which differ from no-row GET model
	// defaults for extraction/role. The source SQLite transition oracle covers it.
	return binding, err
}
func checkSettingRevision(ctx context.Context, tx *sql.Tx, binding Binding, expected int64) error {
	var revision int64
	query := fmt.Sprintf(`SELECT %s FROM %s WHERE %s=? FOR UPDATE`, binding.RevisionColumn, binding.Table, binding.KeyColumn)
	if err := tx.QueryRowContext(ctx, query, binding.KeyValue).Scan(&revision); err != nil {
		return err
	}
	if revision != expected {
		return ErrConflict
	}
	return nil
}
func settingUpdated(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
func (r Repository) SavePrompt(ctx context.Context, kind string, input TextInput) (Prompt, error) {
	if err := ValidatePrompt(kind, input); err != nil {
		return Prompt{}, err
	}
	if r.DB == nil {
		return Prompt{}, ErrUnavailable
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Prompt{}, err
	}
	defer tx.Rollback()
	binding, err := bootstrapSetting(ctx, tx, kind, false)
	if err != nil {
		return Prompt{}, err
	}
	if err = checkSettingRevision(ctx, tx, binding, input.Revision); err != nil {
		return Prompt{}, err
	}
	query := fmt.Sprintf(`UPDATE %s SET markdown=?,%s=%s+1,updated_at=UTC_TIMESTAMP() WHERE %s=? AND %s=?`, binding.Table, binding.RevisionColumn, binding.RevisionColumn, binding.KeyColumn, binding.RevisionColumn)
	result, err := tx.ExecContext(ctx, query, input.Text, binding.KeyValue, input.Revision)
	if err != nil {
		return Prompt{}, err
	}
	if err = settingUpdated(result); err != nil {
		return Prompt{}, err
	}
	saved, err := readPromptRow(ctx, tx, kind)
	if err != nil {
		return Prompt{}, err
	}
	saved.MaxLength = PromptLimit(kind)
	if err = tx.Commit(); err != nil {
		return Prompt{}, err
	}
	return saved, nil
}

// SaveModel validates storage shape. Provider catalogs/capabilities must be
// checked by the HTTP/service gate before calling it; no provider is called here.
func (r Repository) SaveModel(ctx context.Context, kind string, input ModelInput) (Model, error) {
	if err := ValidateModel(kind, input); err != nil {
		return Model{}, err
	}
	if r.DB == nil {
		return Model{}, ErrUnavailable
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Model{}, err
	}
	defer tx.Rollback()
	binding, err := bootstrapSetting(ctx, tx, kind, true)
	if err != nil {
		return Model{}, err
	}
	if err = checkSettingRevision(ctx, tx, binding, input.Revision); err != nil {
		return Model{}, err
	}
	columns := "provider=?,openai_model=?,reasoning_effort=?"
	args := []any{input.Provider, input.OpenAIModel, input.ReasoningEffort}
	if kind != "vacancy_extraction" {
		columns += ",openrouter_model=?"
		args = append(args, input.OpenRouterModel)
	}
	query := fmt.Sprintf(`UPDATE %s SET %s,%s=%s+1,updated_at=UTC_TIMESTAMP() WHERE %s=? AND %s=?`, binding.Table, columns, binding.RevisionColumn, binding.RevisionColumn, binding.KeyColumn, binding.RevisionColumn)
	args = append(args, binding.KeyValue, input.Revision)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return Model{}, err
	}
	if err = settingUpdated(result); err != nil {
		return Model{}, err
	}
	saved, err := readModelRow(ctx, tx, kind)
	if err != nil {
		return Model{}, err
	}
	if err = tx.Commit(); err != nil {
		return Model{}, err
	}
	return saved, nil
}
