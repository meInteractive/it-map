package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/roles"
)

type RoleReader interface {
	Read(context.Context) (roles.Catalog, error)
}

var roleDescendantsReadPath = regexp.MustCompile(`^/api/vacancy-roles/([0-9]+)/descendants$`)

// Исходный router проверяет pathname до декодирования percent-encoded сегментов.
func roleReadPath(r *http.Request, view string) (string, bool) {
	path := r.URL.EscapedPath()
	if view == "descendants" {
		match := roleDescendantsReadPath.FindStringSubmatch(path)
		if match == nil {
			return "", false
		}
		return match[1], true
	}
	expected := "/api/vacancy-roles"
	if view == "roots" || view == "leaves" {
		expected += "/" + view
	}
	return "", path == expected
}

func VacancyRoleHandler(m *auth.Manager, repo RoleReader, view string) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		id, validPath := roleReadPath(r, view)
		if !validPath {
			w.Header().Set("Content-Type", "text/plain;charset=UTF-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Not found"))
			return
		}
		if repo == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Хранилище ролей пока не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		data, err := repo.Read(ctx)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Справочник ролей временно недоступен."})
			return
		}
		switch view {
		case "roots":
			jsonReply(w, http.StatusOK, map[string]any{"roles": data.Roots})
		case "leaves":
			jsonReply(w, http.StatusOK, map[string]any{"roles": data.Leaves})
		case "descendants":
			parsed, e := strconv.ParseInt(id, 10, 64)
			if e != nil || parsed < 1 {
				jsonReply(w, http.StatusNotFound, map[string]any{"error": "Роль не найдена."})
				return
			}
			for _, role := range data.Roles {
				if role.ID == parsed {
					selected := map[int64]bool{}
					for _, id := range role.DescendantIDs {
						selected[id] = true
					}
					subset := make([]roles.Role, 0)
					for _, item := range data.Roles {
						if selected[item.ID] {
							subset = append(subset, item)
						}
					}
					jsonReply(w, http.StatusOK, map[string]any{"role": role, "roles": subset})
					return
				}
			}
			jsonReply(w, http.StatusNotFound, map[string]any{"error": "Роль не найдена."})
		default:
			jsonReply(w, http.StatusOK, data)
		}
	})
}
