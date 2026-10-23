package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
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
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return "", false, errors.New("invalid content type")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return "", false, errors.New("expected JSON object")
	}
	key := ""
	var value bool
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", false, err
		}
		next, ok := tok.(string)
		if !ok || (next != "favorite" && next != "hidden") || key != "" {
			return "", false, errors.New("exactly one flag required")
		}
		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			return "", false, err
		}
		if string(raw) != "true" && string(raw) != "false" {
			return "", false, errors.New("expected boolean")
		}
		if err = json.Unmarshal(raw, &value); err != nil {
			return "", false, err
		}
		key = next
	}
	last, err := dec.Token()
	if err != nil || last != json.Delim('}') || key == "" {
		return "", false, errors.New("invalid flag object")
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return "", false, errors.New("trailing JSON input")
	}
	return key, value, nil
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
