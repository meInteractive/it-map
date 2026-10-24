package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
)

type VacancyApplicationCreator interface {
	ApplyToVacancy(context.Context, string, int64, applications.VacancyApplyInput) (applications.VacancyApplyResult, error)
}

// VacancyApplyHandler preserves the short single-click response expected by
// the existing React list. The same handler serves /apply and /apply/create.
func VacancyApplyHandler(m *auth.Manager, repo VacancyApplicationCreator) http.HandlerFunc {
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
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			jsonReply(w, 400, map[string]string{"error": "Некорректный ID вакансии."})
			return
		}
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		dec := json.NewDecoder(r.Body)
		var input applications.VacancyApplyInput
		if err := dec.Decode(&input); err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		var tail any
		if err := dec.Decode(&tail); err != io.EOF {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		result, err := repo.ApplyToVacancy(ctx, user.Name, id, input)
		switch {
		case err == nil:
			jsonReply(w, 200, result)
		case errors.Is(err, applications.ErrCreationContextRequired):
			jsonReply(w, 428, map[string]string{"error": "Для создания нужен ключ и контекст запроса.", "code": "creation_context_required"})
		case errors.Is(err, applications.ErrCreationContextExpired):
			jsonReply(w, 409, map[string]string{"error": "Контекст создания недействителен или устарел.", "code": "creation_context_expired"})
		case errors.Is(err, applications.ErrIntentConflict):
			jsonReply(w, 409, map[string]string{"error": "Этот запрос уже сохранён с другими данными.", "code": "intent_conflict"})
		case errors.Is(err, applications.ErrVacancyOwnerChanged):
			jsonReply(w, 409, map[string]string{"error": "Владелец вакансии изменился. Обновите список.", "code": "vacancy_owner_changed"})
		case errors.Is(err, applications.ErrAmbiguousVacancyLink):
			jsonReply(w, 409, map[string]string{"error": "Обнаружены несколько откликов на вакансию. Обратитесь к администратору."})
		case errors.Is(err, applications.ErrApplicationDeleted):
			jsonReply(w, 410, map[string]string{"error": "Ранее созданный отклик удалён.", "code": "application_deleted"})
		case errors.Is(err, applications.ErrShortcutVacancyMissing):
			jsonReply(w, 404, map[string]string{"error": "Вакансия не найдена."})
		case errors.Is(err, applications.ErrApplicationLimit), errors.Is(err, applications.ErrDuplicateVacancy):
			jsonReply(w, 400, map[string]string{"error": "Невозможно создать ещё один отклик."})
		case errors.Is(err, applications.ErrInvalidInput):
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные отклика."})
		default:
			jsonReply(w, 503, map[string]string{"error": "Не удалось создать отклик."})
		}
	})
}
