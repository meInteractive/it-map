package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"itmap/internal/parser"
)

type VacancyRelevanceCalculator interface {
	Recalculate(context.Context, string, int64) (parser.ManualRelevanceResult, error)
}

func vacancyRelevanceHandler(manager *auth.Manager, service VacancyRelevanceCalculator) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, http.StatusUnauthorized, map[string]string{"error": "Требуется авторизация."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 || id > 9007199254740991 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректный ID вакансии."})
			return
		}
		if r.Body != nil {
			if body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16)); err != nil || len(body) > 0 {
				jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Расчёт использует сохранённые данные и не принимает тело запроса."})
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		if service == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Расчёт релевантности недоступен."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
		defer cancel()
		result, err := service.Recalculate(ctx, user.Name, id)
		if err == nil {
			jsonReply(w, http.StatusOK, result)
			return
		}
		status, message := http.StatusServiceUnavailable, "Не удалось рассчитать релевантность."
		var provider *aiproviders.ServiceError
		switch {
		case errors.Is(err, parser.ErrManualRelevanceMissing):
			status, message = http.StatusNotFound, "Вакансия не найдена."
		case errors.Is(err, parser.ErrManualRelevanceBusy):
			status, message = http.StatusConflict, "Для этих данных уже есть незавершённый запрос на расчёт."
		case errors.Is(err, parser.ErrRelevanceChanged):
			status, message = http.StatusConflict, "Данные изменились во время расчёта. Оценка не сохранена."
		case errors.Is(err, parser.ErrJobInput):
			status, message = http.StatusConflict, "Для расчёта нужны описание вакансии, резюме и настроенный промт с моделью."
		case errors.Is(err, parser.ErrSnapshotInvalid):
			status, message = http.StatusBadGateway, "Модель не вернула оценку от 0 до 100."
		case errors.As(err, &provider):
			status, message = http.StatusBadGateway, "Модель недоступна. Проверьте настройки AI."
		}
		jsonReply(w, status, map[string]string{"error": message})
	})
}
