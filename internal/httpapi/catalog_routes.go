package httpapi

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"itmap/internal/catalog"
)

type CatalogSearcher interface {
	SearchCompanies(context.Context, string) ([]catalog.Match, error)
}

func CompanySearchHandler(searcher CatalogSearcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		if utf8.RuneCountInString(query) > 256 {
			query = string([]rune(query)[:256])
		}
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
