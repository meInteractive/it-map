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

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type ManualVacancyCreator interface {
	CreateManual(context.Context, string, vacancies.Input) ([]vacancies.CompanyVacancy, error)
}

// ManualVacancyCreateHandler preserves v316's POST /api/vacancies contract.
// The handler is admin-only, checks CSRF provenance and never echoes SQL errors.
func ManualVacancyCreateHandler(m *auth.Manager, repo ManualVacancyCreator) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		var payload struct {
			vacancies.Input
			CoverLetterScope string `json:"coverLetterScope"`
		}
		decoder := json.NewDecoder(r.Body)
		if err = decoder.Decode(&payload); err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		if strings.EqualFold(payload.CoverLetterScope, "personal") {
			jsonReply(w, 400, map[string]string{"error": "Личное письмо нельзя копировать в общие поля новой вакансии."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		rows, err := repo.CreateManual(ctx, user.Name, payload.Input)
		switch {
		case errors.Is(err, vacancies.ErrChannelNotFound) && strings.EqualFold(strings.TrimSpace(payload.EntityType), "channel"):
			jsonReply(w, 404, map[string]string{"error": "Telegram-канал не найден."})
		case errors.Is(err, vacancies.ErrInvalidVacancyInput), errors.Is(err, vacancies.ErrInvalidRole), errors.Is(err, vacancies.ErrInvalidChannel):
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные вакансии, роли или Telegram-источника."})
		case errors.Is(err, vacancies.ErrInvalidOwner):
			jsonReply(w, 404, map[string]string{"error": "Компания, агентство или Telegram-канал не найдены."})
		case errors.Is(err, vacancies.ErrDuplicateURL):
			jsonReply(w, 400, map[string]string{"error": "Вакансия с такой ссылкой уже сохранена."})
		case errors.Is(err, vacancies.ErrDuplicateMessage):
			jsonReply(w, 400, map[string]string{"error": "Это Telegram-сообщение уже связано с другой вакансией."})
		case errors.Is(err, vacancies.ErrWriteBusy):
			jsonReply(w, 503, map[string]string{"error": "Создание вакансии занято, попробуйте ещё раз."})
		case err != nil:
			jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить вакансию."})
		default:
			jsonReply(w, 201, map[string]any{"ok": true, "vacancies": rows})
		}
	})
}
