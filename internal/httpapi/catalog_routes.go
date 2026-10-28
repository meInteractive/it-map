package httpapi

import (
	"context"
	"encoding/json"
	"itmap/internal/backup"
	"net/http"
	"time"
)

type CatalogSearcher interface {
	SearchCompanies(context.Context, json.RawMessage) (json.RawMessage, error)
}

func CompanySearchHandler(searcher CatalogSearcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !catalogReadMethod(w, r) {
			return
		}
		query := backup.CatalogSearchQuery(backup.CatalogQueryValue(r.URL.RawQuery, "q"))
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		matches, err := searcher.SearchCompanies(ctx, query)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Каталог временно недоступен."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"matches": matches})
	}
}

// Called inside the session guard: source authenticates before rejecting a method.
func catalogReadMethod(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, map[string]any{"error": "Метод не поддерживается."})
		return false
	}
	return true
}
