package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type VacancyFlagWriter interface {
	SetUserFlag(context.Context, string, int64, string, bool) (vacancies.UserFlags, error)
}

// Same-origin validation is necessary even with SameSite=Strict cookies: the
// migration must not introduce CSRF opportunities on state-mutating APIs.
func writeSameOrigin(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "same-site") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // Non-browser API clients can omit Origin.
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host == r.Host && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}

func parseFlagUpdate(w http.ResponseWriter, r *http.Request) (string, bool, error) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		return "", false, errors.New("content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	var payload map[string]json.RawMessage
	if err := dec.Decode(&payload); err != nil || len(payload) != 1 {
		return "", false, errors.New("invalid flag payload")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return "", false, errors.New("multiple payloads")
	}
	for key, raw := range payload {
		if key != "favorite" && key != "hidden" {
			return "", false, errors.New("invalid key")
		}
		var b bool
		if string(raw) != "true" && string(raw) != "false" {
			return "", false, errors.New("not boolean")
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			return "", false, err
		}
		return key, b, nil
	}
	return "", false, errors.New("empty payload")
}

func VacancyStateHandler(m *auth.Manager, repo VacancyFlagWriter) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID вакансии."})
			return
		}
		key, val, err := parseFlagUpdate(w, r)
		if err != nil {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Укажите один изменяемый признак вакансии."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		state, err := repo.SetUserFlag(ctx, user.Name, id, key, val)
		switch {
		case errors.Is(err, vacancies.ErrNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Вакансия не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось изменить признаки вакансии."})
		default:
			jsonReply(w, http.StatusOK, struct {
				OK bool `json:"ok"`
				vacancies.UserFlags
			}{true, state})
		}
	})
}
