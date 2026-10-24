package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/tasks"
)

type TaskCreator interface {
	Create(context.Context, string, tasks.CreateInput) (int64, error)
}

func parseTaskCreation(w http.ResponseWriter, r *http.Request) (tasks.CreateInput, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return tasks.CreateInput{}, errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	d := json.NewDecoder(r.Body)
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return tasks.CreateInput{}, errors.New("expected object")
	}
	var input tasks.CreateInput
	seen := map[string]bool{}
	for d.More() {
		keyToken, e := d.Token()
		if e != nil {
			return tasks.CreateInput{}, e
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return tasks.CreateInput{}, errors.New("unknown or duplicate field")
		}
		seen[key] = true
		switch key {
		case "companyName":
			e = d.Decode(&input.CompanyName)
		case "vacancyId":
			e = d.Decode(&input.VacancyID)
		case "type":
			e = d.Decode(&input.Type)
		case "note":
			e = d.Decode(&input.Note)
		case "dueAt":
			e = d.Decode(&input.DueAt)
		default:
			return tasks.CreateInput{}, errors.New("unknown field")
		}
		if e != nil {
			return tasks.CreateInput{}, e
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return tasks.CreateInput{}, errors.New("expected closing brace")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return tasks.CreateInput{}, errors.New("trailing input")
	}
	if !taskDeadlinePattern.MatchString(input.DueAt) {
		return tasks.CreateInput{}, errors.New("invalid deadline")
	}
	if _, err = time.Parse(time.RFC3339Nano, input.DueAt); err != nil {
		return tasks.CreateInput{}, errors.New("invalid deadline")
	}
	return input, nil
}

func TaskCreateHandler(m *auth.Manager, creator TaskCreator) http.HandlerFunc {
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
		input, err := parseTaskCreation(w, r)
		if err != nil {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректные данные задачи."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		id, err := creator.Create(ctx, user.Name, input)
		switch {
		case errors.Is(err, tasks.ErrInvalidTask):
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Некорректные данные задачи."})
		case errors.Is(err, tasks.ErrCompanyNotFound):
			jsonReply(w, http.StatusNotFound, map[string]string{"error": "Компания не найдена."})
		case errors.Is(err, tasks.ErrVacancyNotInCompany):
			jsonReply(w, http.StatusBadRequest, map[string]string{"error": "Выбранная вакансия не относится к этой компании."})
		case errors.Is(err, tasks.ErrCalendarSyncRequired):
			jsonReply(w, http.StatusConflict, map[string]string{"error": "Для создания задачи сначала необходимо перенести синхронизацию Google Calendar."})
		case err != nil:
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error": "Не удалось создать задачу."})
		default:
			jsonReply(w, http.StatusCreated, map[string]any{"ok": true, "taskId": id, "calendarSync": map[string]string{"status": "not_connected"}})
		}
	})
}
