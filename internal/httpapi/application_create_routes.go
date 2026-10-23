package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type ApplicationCreator interface {
	CreationContext(context.Context, string) (string, error)
	CreateWithReceipt(context.Context, string, applications.CreateInput) (applications.CreationResult, error)
}

func ApplicationCreateContextHandler(m *auth.Manager, repo ApplicationCreator) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		token, err := repo.CreationContext(ctx, user.Name)
		if err != nil {
			jsonReply(w, 503, map[string]string{"error": "Не удалось подготовить создание отклика."})
			return
		}
		jsonReply(w, 200, map[string]string{"creationContext": token})
	})
}

func ApplicationCreateHandler(m *auth.Manager, repo ApplicationCreator) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(m, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
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
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		var input applications.CreateInput
		dec := json.NewDecoder(r.Body)
		if err = dec.Decode(&input); err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		var excess any
		if err = dec.Decode(&excess); err != io.EOF {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		result, err := repo.CreateWithReceipt(ctx, user.Name, input)
		if err != nil {
			code := 0
			status := 503
			message := "Не удалось создать отклик."
			switch {
			case errors.Is(err, applications.ErrCreationContextRequired):
				status = 428
				code = 1
				message = "Для создания нужен ключ и контекст запроса."
			case errors.Is(err, applications.ErrCreationContextExpired):
				status = 409
				code = 2
				message = "Контекст создания недействителен или устарел. Черновик сохранён."
			case errors.Is(err, applications.ErrIntentConflict):
				status = 409
				code = 3
				message = "Этот запрос уже сохранён с другими данными."
			case errors.Is(err, applications.ErrIncompleteReceipt):
				status = 503
				code = 4
				message = "Результат создания неполон. Обратитесь к администратору."
			case errors.Is(err, applications.ErrApplicationDeleted):
				status = 410
				code = 5
				message = "Созданный отклик уже удалён. Повтор не создаст его заново."
			case errors.Is(err, applications.ErrApplicationLimit):
				status = 400
				message = "У компании уже сохранено 10 откликов."
			case errors.Is(err, applications.ErrDuplicateVacancy):
				status = 400
				message = "Для этой вакансии отклик уже создан."
			case errors.Is(err, applications.ErrInvalidVacancy):
				status = 400
				message = "Выбранная вакансия не относится к этой компании."
			case errors.Is(err, applications.ErrCompanyNotFound):
				status = 404
				message = "Компания не найдена."
			case errors.Is(err, applications.ErrInvalidInput):
				status = 400
				message = "Некорректные данные отклика."
			}
			codes := map[int]string{1: "creation_context_required", 2: "creation_context_expired", 3: "intent_conflict", 4: "incomplete_receipt", 5: "application_deleted"}
			if code != 0 {
				jsonReply(w, status, map[string]any{"error": message, "code": codes[code]})
			} else {
				jsonReply(w, status, map[string]string{"error": message})
			}
			return
		}
		if result.Replayed {
			jsonReply(w, 200, result)
		} else {
			jsonReply(w, 201, result)
		}
	})
}
