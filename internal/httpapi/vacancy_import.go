package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/vacancies"
)

type VacancyImporter interface {
	ImportExcel(context.Context, string, vacancies.ImportRequest) (vacancies.ImportResult, error)
}

func vacancyImportHandler(manager *auth.Manager, store VacancyImporter) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		w.Header().Set("Cache-Control", "private, no-store")
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Хранилище вакансий пока не подключено."})
			return
		}
		const maxBytes = 25 * 1024 * 1024
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBytes))
		if err != nil {
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				jsonReply(w, 413, map[string]string{"error": "Данные импорта больше допустимых 25 МБ."})
			} else {
				jsonReply(w, 400, map[string]string{"error": "Не удалось прочитать данные из Excel-файла."})
			}
			return
		}
		var input vacancies.ImportRequest
		if err = json.Unmarshal(body, &input); err != nil {
			jsonReply(w, 400, map[string]string{"error": "Не удалось прочитать данные из Excel-файла."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		result, err := store.ImportExcel(ctx, user.Name, input)
		if err != nil {
			var invalid vacancies.ImportError
			if errors.As(err, &invalid) {
				jsonReply(w, 400, map[string]string{"error": invalid.Error()})
			} else {
				jsonReply(w, 503, map[string]string{"error": "Не удалось импортировать вакансии."})
			}
			return
		}
		jsonReply(w, 200, result)
	})
}
