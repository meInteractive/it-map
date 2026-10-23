package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
)

type CompactCatalog interface {
	ListEntities(context.Context, string, catalog.EntityType, catalog.View) ([]map[string]any, error)
	EntityDetail(context.Context, string, catalog.EntityType, int64, string) (map[string]any, error)
}

func compactListHandler(m *auth.Manager, repo CompactCatalog, kind catalog.EntityType, view catalog.View) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		u, _ := sessionFromRequest(m, r)
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		items, err := repo.ListEntities(ctx, u.Name, kind, view)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Хранилище компаний временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"companies": items})
	})
}
func compactDetailHandler(m *auth.Manager, repo CompactCatalog, kind catalog.EntityType, numeric bool) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		u, _ := sessionFromRequest(m, r)
		var id int64
		var name string
		if numeric {
			str := r.PathValue("id")
			// Unported endpoints such as /companies/import MUST NOT be mistaken
			// for numeric entity detail endpoints.
			if len(str) == 0 || len(str) > 19 || strings.Trim(str, "0123456789") != "" {
				jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "backend_not_migrated"})
				return
			}
			parsed, err := strconv.ParseInt(str, 10, 64)
			if err != nil || parsed < 1 {
				jsonReply(w, http.StatusNotFound, map[string]any{"error": "Компания не найдена."})
				return
			}
			id = parsed
		}
		name = r.URL.Query().Get("name")
		if len(name) > 4096 {
			jsonReply(w, http.StatusBadRequest, map[string]any{"error": "Недопустимое имя организации."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		detail, err := repo.EntityDetail(ctx, u.Name, kind, id, name)
		if errors.Is(err, catalog.ErrNotFound) {
			jsonReply(w, http.StatusNotFound, map[string]any{"error": "Компания не найдена."})
			return
		}
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Хранилище компаний временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"company": detail})
	})
}
