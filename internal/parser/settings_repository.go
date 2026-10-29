package parser

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"itmap/internal/backup"
	"itmap/internal/dbretry"
)

var (
	ErrSettingsChanged    = errors.New("scheduler settings changed")
	ErrSettingsGeneration = errors.New("scheduler settings generation changed")
	ErrSettingsStorage    = errors.New("scheduler settings storage unavailable")
)

// SettingsRepository persists configuration only. It never executes a command,
// reads provider credentials or starts a worker. Authorization belongs to its
// HTTP consumer; ownerName is the authenticated account, never an input field.
type SettingsRepository struct{ DB *sql.DB }
type settingsRow struct {
	raw                []byte
	favorite, revision int64
}

const settingsRowSQL = "SELECT scheduler_json,favorites_only,revision FROM firecrawl_settings WHERE id=1"

func settingsFailure(ctx context.Context, e error) error {
	if e == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var input *SettingsError
	if errors.As(e, &input) || errors.Is(e, ErrSettingsChanged) || errors.Is(e, ErrSettingsGeneration) {
		return e
	}
	return ErrSettingsStorage
}
func readSettingsRow(ctx context.Context, tx *sql.Tx, lock bool) (settingsRow, error) {
	r := settingsRow{raw: []byte(`{"revision":0}`), favorite: 1}
	query := settingsRowSQL
	if lock {
		query += " FOR UPDATE"
	}
	e := tx.QueryRowContext(ctx, query).Scan(&r.raw, &r.favorite, &r.revision)
	if errors.Is(e, sql.ErrNoRows) {
		return settingsRow{raw: []byte(`{"revision":0}`), favorite: 1}, nil
	}
	return r, e
}
func settingsView(ctx context.Context, tx *sql.Tx, row settingsRow) (backup.SchedulerSettingsView, error) {
	view, e := backup.ReadSchedulerSettingsJSON(row.raw, row.favorite != 0, nil)
	if e != nil {
		return view, e
	}
	ids, e := jsonPathFirst(row.raw, []string{"sourceIds"})
	if e != nil || ids != nil {
		return view, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT DISTINCT source_id FROM vacancy_source_parsing WHERE auto_enabled=1")
	if e != nil {
		return view, e
	}
	legacy := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		legacy = append(legacy, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return view, e
	}
	return backup.ReadSchedulerSettingsJSON(row.raw, row.favorite != 0, legacy)
}

func (r SettingsRepository) Read(ctx context.Context) (json.RawMessage, error) {
	if r.DB == nil {
		return nil, ErrSettingsStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return nil, settingsFailure(ctx, e)
	}
	defer tx.Rollback()
	var generation string
	if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); e != nil || !generationPattern.MatchString(generation) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	row, e := readSettingsRow(ctx, tx, false)
	if e != nil {
		return nil, settingsFailure(ctx, e)
	}
	view, e := settingsView(ctx, tx, row)
	if e != nil {
		return nil, settingsFailure(ctx, e)
	}
	if e = tx.Commit(); e != nil {
		return nil, settingsFailure(ctx, e)
	}
	return view.PublicJSON(), nil
}

// Validate the caller with the existing 078 gate, then validate priorities after
// the source raw merge. Other previous fields affect serialization, not input
// admission; they must not be coerced through a lossy typed storage struct.
func validateStoredSettingsInput(input json.RawMessage, view backup.SchedulerSettingsView, ownerName string) (SettingsUpdate, error) {
	base, e := ValidateSettingsUpdate(input, DefaultSettings(), ownerName)
	if e != nil {
		return base, e
	}
	var old map[string]json.RawMessage
	if e = json.Unmarshal(view.PublicJSON(), &old); e != nil {
		return base, ErrSettingsStorage
	}
	priorities, e := readObject(old["priorities"])
	if e != nil {
		return base, &SettingsError{Message: prioritiesMessage}
	}
	raw, e := readObject(input)
	if e != nil {
		return base, e
	}
	if value, ok := raw["priorities"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		patch, e := readObject(value)
		if e != nil {
			return base, e
		}
		for k, v := range patch {
			priorities[k] = v
		}
	}
	previous := DefaultSettings()
	previous.Priorities = map[string]int64{}
	for k, v := range priorities {
		n, ok := integer(v)
		if !ok {
			return base, &SettingsError{Message: prioritiesMessage}
		}
		previous.Priorities[k] = n
	}
	return ValidateSettingsUpdate(input, previous, ownerName)
}

// SQLite JSON1 compares extracted numeric/boolean scalars to the safe integer
// binding without coercing a JSON string. It selects the FIRST duplicate key;
// JSON.parse-based source serialization deliberately retains the LAST instead.
func settingsRevisionMatches(raw []byte, expected int64) (bool, error) {
	value, e := jsonPathFirst(raw, []string{"revision"})
	if e != nil {
		return false, e
	}
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return false, nil
	}
	switch value[0] {
	case '"', '{', '[':
		return false, nil
	case 't':
		return expected == 1, nil
	case 'f':
		return expected == 0, nil
	}
	text := string(value)
	if !strings.ContainsAny(text, ".eE") {
		if n, e := strconv.ParseInt(text, 10, 64); e == nil {
			return n == expected, nil
		}
	}
	n, e := sqliteDecimal(text)
	if e != nil && !errors.Is(e, strconv.ErrRange) {
		return false, e
	}
	return n == float64(expected), nil
}

// PrepareSave фиксирует поколение и владельца до чтения HTTP body.
// Возвращаемая операция не удерживает соединение или SQL-транзакцию.
func (r SettingsRepository) PrepareSave(ctx context.Context, ownerName string) (func(context.Context, json.RawMessage) (json.RawMessage, error), error) {
	if r.DB == nil {
		return nil, ErrSettingsStorage
	}
	if ownerName == "" || !utf8.ValidString(ownerName) {
		return nil, &SettingsError{Message: settingsMessage}
	}
	var expectedGeneration string
	if e := r.DB.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&expectedGeneration); e != nil || !generationPattern.MatchString(expectedGeneration) {
		return nil, settingsFailure(ctx, ErrSettingsStorage)
	}
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		return r.saveInGeneration(ctx, input, ownerName, expectedGeneration)
	}, nil
}

func (r SettingsRepository) Save(ctx context.Context, input json.RawMessage, ownerName string) (json.RawMessage, error) {
	save, e := r.PrepareSave(ctx, ownerName)
	if e != nil {
		return nil, e
	}
	return save(ctx, input)
}

func (r SettingsRepository) saveInGeneration(ctx context.Context, input json.RawMessage, ownerName, expectedGeneration string) (json.RawMessage, error) {
	result, e := dbretry.Run(ctx, func() (json.RawMessage, error) {
		tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if e != nil {
			return nil, e
		}
		defer tx.Rollback()
		var generation string
		if e = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); e != nil {
			return nil, e
		}
		if generation != expectedGeneration {
			return nil, ErrSettingsGeneration
		}
		// The captured MariaDB DEFAULT has broken quotes. Explicit valid JSON is
		// initialized only in this write TX, then rolled back on any failed save.
		if _, e = tx.ExecContext(ctx, `INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,'{"revision":0}') ON DUPLICATE KEY UPDATE id=VALUES(id)`); e != nil {
			return nil, e
		}
		row, e := readSettingsRow(ctx, tx, true)
		if e != nil {
			return nil, e
		}
		view, e := settingsView(ctx, tx, row)
		if e != nil {
			return nil, e
		}
		validated, e := validateStoredSettingsInput(input, view, ownerName)
		if e != nil {
			return nil, e
		}
		change, e := view.NextFromValidated(input, ownerName)
		if e != nil {
			return nil, e
		}
		matches, e := settingsRevisionMatches(row.raw, validated.ExpectedRevision)
		if e != nil {
			return nil, e
		}
		if !matches {
			return nil, ErrSettingsChanged
		}
		raw, e := readObject(input)
		if e != nil {
			return nil, e
		}
		var favorite any
		if value, ok := raw["favoritesOnly"]; ok {
			b, _ := boolean(value)
			if b {
				favorite = int64(1)
			} else {
				favorite = int64(0)
			}
		}
		changed, e := tx.ExecContext(ctx, `UPDATE firecrawl_settings SET scheduler_json=?,revision=revision+CASE WHEN ? IS NOT NULL AND favorites_only<>? THEN 1 ELSE 0 END,favorites_only=coalesce(?,favorites_only) WHERE id=1`, string(change.StorageJSON()), favorite, favorite, favorite)
		if e != nil {
			return nil, e
		}
		affected, e := changed.RowsAffected()
		if e != nil {
			return nil, e
		}
		if affected != 1 {
			return nil, ErrSettingsStorage
		}
		// Source re-reads the effective state after its UPDATE. Keep this within
		// the same TX so a later query failure cannot commit a partial operation.
		row, e = readSettingsRow(ctx, tx, false)
		if e != nil {
			return nil, e
		}
		view, e = settingsView(ctx, tx, row)
		if e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		return view.PublicJSON(), nil
	})
	return result, settingsFailure(ctx, e)
}
