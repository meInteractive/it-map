package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type ApplicationCompanyReader interface {
	CompanyApplications(context.Context, string, string) (applications.CompanyResponse, error)
}

type ApplicationMutator interface {
	Update(context.Context, string, int64, applications.UpdateInput) (string, error)
	Delete(context.Context, string, int64) (string, error)
}

// parseApplicationUpdate accepts the full v316 React application record. The
// frontend sends derived read-only fields along with editable fields, so
// DisallowUnknownFields would break the existing client. Only recognized
// writable properties are decoded into UpdateInput.
func parseApplicationUpdate(w http.ResponseWriter, r *http.Request) (applications.UpdateInput, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return applications.UpdateInput{}, errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	var in applications.UpdateInput
	if err = decoder.Decode(&in); err != nil {
		return in, err
	}
	var tail any
	if err = decoder.Decode(&tail); err != io.EOF {
		return in, errors.New("invalid trailing JSON")
	}
	return in, nil
}

// CompanyApplicationsHandler handles the original company-specific GET.
func CompanyApplicationsHandler(m *auth.Manager, repo ApplicationCompanyReader) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		company := strings.TrimSpace(r.URL.Query().Get("company"))
		if company == "" {
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		result, err := repo.CompanyApplications(ctx, user.Name, company)
		switch {
		case errors.Is(err, applications.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Хранилище откликов временно недоступно."})
		default:
			jsonReply(w, http.StatusOK, result)
		}
	})
}

// ApplicationMutationHandler preserves v316 response shapes for PUT and DELETE.
// The DB mutation commits before re-reading the company response; a post-commit
// read failure is reported rather than claiming success with incomplete data.
func ApplicationMutationHandler(m *auth.Manager, writer ApplicationMutator, reader ApplicationCompanyReader) http.HandlerFunc {
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
		if err != nil || id < 1 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID отклика."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		var company string
		if r.Method == "PUT" {
			input, parseErr := parseApplicationUpdate(w, r)
			if parseErr != nil {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректные данные."})
				return
			}
			company, err = writer.Update(ctx, user.Name, id, input)
		} else if r.Method == "DELETE" {
			company, err = writer.Delete(ctx, user.Name, id)
		} else {
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		switch {
		case errors.Is(err, applications.ErrNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Отклик не найден."})
			return
		case errors.Is(err, applications.ErrRevisionRequired):
			jsonReply(w, http.StatusPreconditionRequired, map[string]string{"error": "Загрузите актуальный отклик: для сохранения нужна версия записи."})
			return
		case errors.Is(err, applications.ErrConflict):
			jsonReply(w, http.StatusConflict, map[string]string{"error": "Отклик изменился. Обновите запись перед сохранением."})
			return
		case errors.Is(err, applications.ErrDuplicateVacancy):
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Для этой вакансии отклик уже создан."})
			return
		case errors.Is(err, applications.ErrInvalidVacancy):
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Выбранная вакансия не относится к этой компании."})
			return
		case errors.Is(err, applications.ErrInvalidInput):
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректные поля отклика."})
			return
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось сохранить отклик."})
			return
		}
		companyResult, err := reader.CompanyApplications(ctx, user.Name, company)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Изменение выполнено, но не удалось обновить список. Перезагрузите страницу."})
			return
		}
		jsonReply(w, http.StatusOK, companyResult)
	})
}
