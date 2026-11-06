package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/integrations/hhpublic"
)

type HHProfessionalRolesReader interface {
	FetchProfessionalRoles(context.Context) (*hhpublic.ProfessionalRoles, error)
}

func HHProfessionalRolesHandler(manager *auth.Manager, reader HHProfessionalRolesReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Справочник HH временно недоступен."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 9*time.Second)
		defer cancel()
		result, err := reader.FetchProfessionalRoles(ctx)
		if err != nil || result == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Справочник HH временно недоступен."})
			return
		}
		jsonReply(w, http.StatusOK, result)
	})
}
