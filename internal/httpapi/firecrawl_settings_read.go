package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
)

type firecrawlConnectionView struct {
	Configured bool    `json:"configured"`
	Status     string  `json:"connectionStatus"`
	CheckedAt  *string `json:"checkedAt"`
}

func firecrawlSettingsRead(manager *auth.Manager, db *sql.DB, canUpdateKey bool) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok || !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		mode, proxy := "free", "basic"
		var free, hobby firecrawlConnectionView
		free.Status, hobby.Status = "missing", "missing"
		var enabled, favorites, enhanced bool
		favorites = true
		var pages, timeout, revision int64 = 3, 45000, 0
		err := sql.ErrConnDone
		if db != nil {
			err = db.QueryRowContext(ctx, `SELECT enabled=1,favorites_only<>0,mode,max_pages,timeout_ms,proxy,enhanced_allowed=1,revision,
				key_encrypted<>'',connection_status,checked_at,hobby_key_encrypted<>'',hobby_connection_status,hobby_checked_at
				FROM firecrawl_settings WHERE id=1`).Scan(&enabled, &favorites, &mode, &pages, &timeout, &proxy, &enhanced, &revision,
				&free.Configured, &free.Status, &free.CheckedAt, &hobby.Configured, &hobby.Status, &hobby.CheckedAt)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось прочитать настройки Firecrawl."})
			return
		}
		selected := free
		if mode == "hobby" {
			selected = hobby
		} else {
			proxy, enhanced = "basic", false
		}
		jsonReply(w, http.StatusOK, map[string]any{
			"readOnly": true, "canUpdateKey": canUpdateKey, "configured": selected.Configured, "connectionStatus": selected.Status, "checkedAt": selected.CheckedAt,
			"mode": mode, "enabled": enabled, "favoritesOnly": favorites, "revision": revision, "maxPages": pages,
			"concurrency": 1, "timeoutMs": timeout, "proxy": proxy, "enhancedAllowed": enhanced,
			"profiles": map[string]firecrawlConnectionView{"free": free, "hobby": hobby},
		})
	})
}
