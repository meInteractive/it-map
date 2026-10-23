package httpapi

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type ManualVacancyUpdater interface {
	UpdateManual(context.Context, string, int64, vacancies.UpdateRequest) ([]vacancies.CompanyVacancy, error)
}

// ManualVacancyUpdateHandler preserves the editor's {ok,vacancies} response.
// Invalid/stale revisions are not converted into generic 503 errors.
func ManualVacancyUpdateHandler(manager *auth.Manager, repo ManualVacancyUpdater) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
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
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		req, err := vacancies.ParseUpdateRequest(data)
		if err != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные вакансии."})
			return
		}
		if err = vacancies.ValidateRevision(req.Revision); err != nil {
			jsonReply(w, 428, map[string]string{"error": "Загрузите актуальную карточку вакансии перед редактированием."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		rows, err := repo.UpdateManual(ctx, user.Name, id, req)
		switch {
		case errors.Is(err, vacancies.ErrVersionConflict):
			jsonReply(w, 409, map[string]string{"error": "Вакансия была изменена. Обновите карточку."})
		case errors.Is(err, vacancies.ErrRevisionRequired):
			jsonReply(w, 428, map[string]string{"error": "Загрузите актуальную карточку вакансии перед редактированием."})
		case errors.Is(err, vacancies.ErrNotFound), errors.Is(err, vacancies.ErrInvalidOwner):
			jsonReply(w, 404, map[string]string{"error": "Вакансия или работодатель не найдены."})
		case errors.Is(err, vacancies.ErrInvalidVacancyInput), errors.Is(err, vacancies.ErrInvalidRole), errors.Is(err, vacancies.ErrInvalidChannel), errors.Is(err, vacancies.ErrDuplicateURL), errors.Is(err, vacancies.ErrDuplicateMessage):
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные или повторяющаяся ссылка вакансии."})
		case errors.Is(err, vacancies.ErrAgencySalaryProjectionPending):
			jsonReply(w, 503, map[string]string{"error": "Редактирование вакансии агентства временно недоступно до переноса пересчёта зарплат."})
		case errors.Is(err, vacancies.ErrLegacyDuplicates), errors.Is(err, vacancies.ErrWriteBusy):
			jsonReply(w, 409, map[string]string{"error": "Неоднозначные существующие записи или параллельное изменение. Проверьте данные и повторите запрос."})
		case err != nil:
			jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить вакансию."})
		default:
			jsonReply(w, 200, map[string]any{"ok": true, "vacancies": rows})
		}
	})
}
