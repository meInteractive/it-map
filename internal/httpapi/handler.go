package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// DBChecker is only used to verify the active DB connection; the readiness gate
// remains closed until the entire API and parser have been accepted.
type DBChecker interface{ PingContext(context.Context) error }

// NewHandler returns the migration shell. No legacy business API returns success
// until its handler has been ported and tested against the frozen contract.
func NewHandler(logger *slog.Logger, database ...DBChecker) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, http.StatusOK, map[string]any{"status": "alive"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		reason := "mariadb_not_connected"
		if len(database) > 0 && database[0] != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := database[0].PingContext(ctx); err != nil {
				reason = "mariadb_unavailable"
			} else {
				reason = "business_api_not_migrated"
			}
		}
		jsonReply(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready", "reason": reason})
	})
	unmigrated := func(w http.ResponseWriter, _ *http.Request) {
		jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "backend_not_migrated"})
	}
	mux.HandleFunc("/api/", unmigrated)
	mux.HandleFunc("/auth", unmigrated)
	mux.HandleFunc("/logout", unmigrated)
	mux.HandleFunc("/mcp", unmigrated)
	mux.HandleFunc("/", unmigrated)
	return recoverMiddleware(logger, safeHeaders(mux))
}

func jsonReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func safeHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				logger.Error("http request panic", "method", r.Method, "path", r.URL.Path)
				jsonReply(w, http.StatusInternalServerError, map[string]any{"error": "internal_error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
