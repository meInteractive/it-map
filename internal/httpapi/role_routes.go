package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/roles"
)

type RoleReader interface {
	Read(context.Context) (roles.Catalog, error)
}

func VacancyRoleHandler(m *auth.Manager, repo RoleReader, view string) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
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
			parsed, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if e != nil || parsed < 1 {
				jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "backend_not_migrated"})
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
