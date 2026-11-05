package parser

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
)

// HHRoleSelection хранится внутри scheduler_json и входит в существующий бэкап.
type HHRoleSelection struct {
	Revision int64    `json:"revision"`
	RoleIDs  []string `json:"roleIds"`
}

var hhProfessionalID = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
var ErrHHRoleSettings = errors.New("некорректные настройки профессиональных ролей HH")

func normalizeHHRoleIDs(ids []string) ([]string, error) {
	if ids == nil || len(ids) > 500 {
		return nil, ErrHHRoleSettings
	}
	seen := make(map[string]bool, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if !hhProfessionalID.MatchString(id) || seen[id] {
			return nil, ErrHHRoleSettings
		}
		seen[id] = true
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		left, _ := strconv.ParseInt(result[i], 10, 64)
		right, _ := strconv.ParseInt(result[j], 10, 64)
		return left < right
	})
	return result, nil
}

// updateHHRoleSelection сохраняет все остальные настройки планировщика и
// проверяет независимую версию выбранной крупной роли.
func updateHHRoleSelection(raw []byte, rootID int64, revision int64, ids []string) ([]byte, HHRoleSelection, error) {
	if rootID < 1 || revision < 0 {
		return nil, HHRoleSelection{}, ErrHHRoleSettings
	}
	normalized, err := normalizeHHRoleIDs(ids)
	if err != nil {
		return nil, HHRoleSelection{}, err
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return nil, HHRoleSelection{}, ErrHHRoleSettings
	}
	var schedulerRevision int64
	if value := data["revision"]; value != nil {
		if json.Unmarshal(value, &schedulerRevision) != nil || schedulerRevision < 0 || schedulerRevision >= 9007199254740991 {
			return nil, HHRoleSelection{}, ErrHHRoleSettings
		}
	}
	roles := map[string]HHRoleSelection{}
	if value := data["hhProfessionalRoles"]; value != nil {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &roles) != nil || roles == nil {
			return nil, HHRoleSelection{}, ErrHHRoleSettings
		}
	}
	key := strconv.FormatInt(rootID, 10)
	current := roles[key]
	if current.Revision != revision {
		return nil, HHRoleSelection{}, ErrSettingsChanged
	}
	if revision == 9007199254740991 {
		return nil, HHRoleSelection{}, ErrHHRoleSettings
	}
	next := HHRoleSelection{Revision: revision + 1, RoleIDs: normalized}
	roles[key] = next
	value, err := json.Marshal(roles)
	if err != nil {
		return nil, HHRoleSelection{}, ErrHHRoleSettings
	}
	data["hhProfessionalRoles"] = value
	data["revision"] = json.RawMessage(strconv.FormatInt(schedulerRevision+1, 10))
	updated, err := json.Marshal(data)
	if err != nil {
		return nil, HHRoleSelection{}, ErrHHRoleSettings
	}
	return updated, next, nil
}

type HHRoleSettingsRepository struct{ DB *sql.DB }

type HHRoleSetting struct {
	RoleID   int64    `json:"roleId"`
	Name     string   `json:"name"`
	Revision int64    `json:"revision"`
	RoleIDs  []string `json:"roleIds"`
}

type HHRoleSettingsView struct {
	Generation string          `json:"generation"`
	Roles      []HHRoleSetting `json:"roles"`
}

func (r HHRoleSettingsRepository) Read(ctx context.Context) (HHRoleSettingsView, error) {
	result := HHRoleSettingsView{Roles: []HHRoleSetting{}}
	if r.DB == nil {
		return result, ErrSettingsStorage
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, ErrSettingsStorage
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&result.Generation); err != nil || !generationPattern.MatchString(result.Generation) {
		return result, ErrSettingsStorage
	}
	row, err := readSettingsRow(ctx, tx, false)
	if err != nil {
		return result, ErrSettingsStorage
	}
	var saved struct {
		Roles map[string]HHRoleSelection `json:"hhProfessionalRoles"`
	}
	if json.Unmarshal(row.raw, &saved) != nil {
		return result, ErrSettingsStorage
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,name FROM vacancy_roles WHERE parent_id IS NULL ORDER BY sort_order,id")
	if err != nil {
		return result, ErrSettingsStorage
	}
	for rows.Next() {
		var role HHRoleSetting
		if err = rows.Scan(&role.RoleID, &role.Name); err != nil {
			break
		}
		value, exists := saved.Roles[strconv.FormatInt(role.RoleID, 10)]
		if exists {
			if value.Revision < 1 || value.Revision > 9007199254740991 {
				err = ErrSettingsStorage
				break
			}
			role.RoleIDs, err = normalizeHHRoleIDs(value.RoleIDs)
			if err != nil {
				break
			}
			role.Revision = value.Revision
		} else {
			role.RoleIDs = []string{}
		}
		result.Roles = append(result.Roles, role)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || tx.Commit() != nil {
		return HHRoleSettingsView{}, ErrSettingsStorage
	}
	return result, nil
}

func (r HHRoleSettingsRepository) Save(ctx context.Context, generation string, rootID, revision int64, ids []string) (HHRoleSetting, error) {
	var result HHRoleSetting
	if r.DB == nil {
		return result, ErrSettingsStorage
	}
	if !generationPattern.MatchString(generation) {
		return result, ErrHHRoleSettings
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, ErrSettingsStorage
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&current); err != nil {
		return result, ErrSettingsStorage
	}
	if current != generation {
		return result, ErrSettingsGeneration
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,'{"revision":0}') ON DUPLICATE KEY UPDATE id=VALUES(id)`); err != nil {
		return result, ErrSettingsStorage
	}
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return result, ErrSettingsStorage
	}
	if err = tx.QueryRowContext(ctx, "SELECT id,name FROM vacancy_roles WHERE id=? AND parent_id IS NULL FOR UPDATE", rootID).Scan(&result.RoleID, &result.Name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return result, ErrHHRoleSettings
		}
		return result, ErrSettingsStorage
	}
	updated, selection, err := updateHHRoleSelection(row.raw, rootID, revision, ids)
	if err != nil {
		return result, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE firecrawl_settings SET scheduler_json=? WHERE id=1", string(updated)); err != nil {
		return result, ErrSettingsStorage
	}
	if err = tx.Commit(); err != nil {
		return result, ErrSettingsStorage
	}
	result.Revision = selection.Revision
	result.RoleIDs = selection.RoleIDs
	return result, nil
}
