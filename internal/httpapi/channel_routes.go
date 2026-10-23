package httpapi

import (
	"context"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/channels"
)

type ChannelLister interface {
	List(context.Context) ([]channels.Channel, error)
}

func ChannelListHandler(m *auth.Manager, repo ChannelLister) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		items, err := repo.List(ctx)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Хранилище каналов временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"channels": items})
	})
}
