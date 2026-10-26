package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"itmap/internal/auth"
	"itmap/internal/crm"
)

type CompanyCRMWriter interface {
	SaveCompany(context.Context, string, string, crm.SaveInput) (crm.SaveResult, error)
}

// parseCompanySave accepts the typed UI form with omitted/null defaults. The
// signed session is the only owner source; duplicate/unknown fields are refused.
func parseCompanySave(w http.ResponseWriter, r *http.Request) (crm.SaveInput, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return crm.SaveInput{}, errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return crm.SaveInput{}, errors.New("expected object")
	}
	var input crm.SaveInput
	seen := map[string]bool{}
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return input, err
		}
		key, ok := k.(string)
		if !ok || seen[key] {
			return input, errors.New("duplicate field")
		}
		seen[key] = true
		switch key {
		case "status":
			err = d.Decode(&input.Status)
		case "comment":
			err = d.Decode(&input.Comment)
		default:
			return input, errors.New("unknown field")
		}
		if err != nil {
			return input, err
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return input, errors.New("expected closing brace")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return input, errors.New("trailing input")
	}
	return input, input.Validate()
}

func CompanyCRMWriteHandler(manager *auth.Manager, repo CompanyCRMWriter) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		company := strings.TrimSpace(r.URL.Query().Get("company"))
		if company == "" || utf8.RuneCountInString(company) > 255 {
			jsonReply(w, 400, map[string]string{"error": "Некорректная компания."})
			return
		}
		input, err := parseCompanySave(w, r)
		if err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректный статус или комментарий CRM."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		result, err := repo.SaveCompany(ctx, user.Name, company, input)
		switch {
		case errors.Is(err, crm.ErrCompanyNotFound):
			jsonReply(w, 404, map[string]string{"error": "Компания не найдена."})
		case errors.Is(err, crm.ErrInvalidCompanyInput):
			jsonReply(w, 400, map[string]string{"error": "Некорректный статус или комментарий CRM."})
		case errors.Is(err, crm.ErrAmbiguousState):
			jsonReply(w, 409, map[string]string{"error": "Обнаружены дублирующиеся записи CRM. Требуется проверка базы данных."})
		case err != nil:
			jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить CRM компании."})
		default:
			jsonReply(w, 200, result)
		}
	})
}
