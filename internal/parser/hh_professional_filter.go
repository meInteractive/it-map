package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
)

// hhProfessionalRoleUnion не разрешает новый запрос, если хотя бы у одного
// активного направления отсутствует сохранённый непустой выбор.
func hhProfessionalRoleUnion(raw []byte, roots []int64) ([]string, error) {
	if len(roots) == 0 {
		return nil, ErrHHRoleFilter
	}
	var document struct {
		Roles map[string]HHRoleSelection `json:"hhProfessionalRoles"`
	}
	if json.Unmarshal(raw, &document) != nil || document.Roles == nil {
		return nil, ErrHHRoleFilter
	}
	seen := map[string]bool{}
	out := []string{}
	for _, root := range roots {
		selection, ok := document.Roles[strconv.FormatInt(root, 10)]
		if !ok || selection.Revision < 1 || selection.Revision > maxSafeInteger {
			return nil, ErrHHRoleFilter
		}
		ids, err := normalizeHHRoleIDs(selection.RoleIDs)
		if err != nil || len(ids) == 0 {
			return nil, ErrHHRoleFilter
		}
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.ParseInt(out[i], 10, 64)
		b, _ := strconv.ParseInt(out[j], 10, 64)
		return a < b
	})
	return out, nil
}

func hhCurrentProfessionalRoles(ctx context.Context, tx *sql.Tx) ([]string, error) {
	row, err := readSettingsRow(ctx, tx, true)
	if err != nil {
		return nil, ErrSettingsStorage
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.id,c.markdown FROM vacancy_roles r JOIN vacancy_role_classification c ON c.role_id=r.id WHERE r.parent_id IS NULL AND c.parsing_enabled=1 ORDER BY r.sort_order,r.id FOR UPDATE`)
	if err != nil {
		return nil, ErrSettingsStorage
	}
	roots := []int64{}
	for rows.Next() {
		var id int64
		var markdown string
		if err = rows.Scan(&id, &markdown); err != nil {
			break
		}
		if jsStageTrim(markdown) != "" {
			roots = append(roots, id)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, ErrSettingsStorage
	}
	return hhProfessionalRoleUnion(row.raw, roots)
}
