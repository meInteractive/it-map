package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type ResumeStore interface {
	ReadResume(context.Context, string) (profiles.Resume, error)
	SaveResume(context.Context, string, profiles.TextInput) (profiles.Resume, error)
}

func profileInputBody(w http.ResponseWriter, r *http.Request, limit int) ([]byte, error) {
	if r.ContentLength > int64(limit) {
		return nil, &profiles.InputError{Status: 413, Message: "Запрос слишком большой."}
	}
	if r.Body == nil {
		return nil, &profiles.InputError{Status: 400, Message: "Передайте данные профиля."}
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(limit))
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			return nil, &profiles.InputError{Status: 413, Message: "Запрос слишком большой."}
		}
		return nil, &profiles.InputError{Status: 400, Message: "Некорректные данные."}
	}
	return body, nil
}
func profileInputError(w http.ResponseWriter, err error) {
	var input *profiles.InputError
	if errors.As(err, &input) {
		jsonReply(w, input.Status, map[string]string{"error": input.Message})
		return
	}
	jsonReply(w, 400, map[string]string{"error": "Некорректные данные профиля."})
}

type PortfolioStore interface {
	ReadPortfolio(context.Context, string) (profiles.Resume, error)
	SavePortfolio(context.Context, string, profiles.TextInput) (profiles.Resume, error)
}

type profileTextHandlers struct {
	bodyLimit   int
	name, title string
	read        func(context.Context, string) (profiles.Resume, error)
	save        func(context.Context, string, profiles.TextInput) (profiles.Resume, error)
	parse       func([]byte, int64) (profiles.TextInput, error)
	response    func(profiles.Resume) any
}

func ProfileResumeHandler(manager *auth.Manager, store ResumeStore) http.HandlerFunc {
	editor := profileTextHandlers{bodyLimit: profiles.ResumeBodyLimit, name: "резюме", title: "Резюме", parse: profiles.ParseResume, response: func(result profiles.Resume) any { return result }}
	if store != nil {
		editor.read, editor.save = store.ReadResume, store.SaveResume
	}
	return profileTextHandler(manager, editor)
}

func profilePortfolioHandler(manager *auth.Manager, store PortfolioStore) http.HandlerFunc {
	editor := profileTextHandlers{bodyLimit: profiles.PortfolioBodyLimit, name: "портфолио", title: "Портфолио", parse: profiles.ParsePortfolio, response: func(result profiles.Resume) any {
		return map[string]any{"portfolioMd": result.ResumeMD, "revision": result.Revision, "updatedAt": result.UpdatedAt, "maxLength": result.MaxLength}
	}}
	if store != nil {
		editor.read, editor.save = store.ReadPortfolio, store.SavePortfolio
	}
	return profileTextHandler(manager, editor)
}

func profileTextHandler(manager *auth.Manager, editor profileTextHandlers) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok {
			jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
			return
		}
		status := profiles.AccessStatus("resume", "", r.Method, true, user.IsAdmin, editor.read != nil)
		if status == 405 {
			w.Header().Set("Allow", "GET, PUT")
			jsonReply(w, status, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if status != 200 {
			jsonReply(w, status, map[string]string{"error": "Хранилище " + editor.name + " пока не подключено."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if r.Method == "GET" {
			result, err := editor.read(ctx, user.Name)
			if err != nil {
				jsonReply(w, 503, map[string]string{"error": "Не удалось прочитать " + editor.name + "."})
				return
			}
			jsonReply(w, 200, editor.response(result))
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		body, err := profileInputBody(w, r, editor.bodyLimit)
		if err != nil {
			profileInputError(w, err)
			return
		}
		input, err := editor.parse(body, r.ContentLength)
		if err != nil {
			profileInputError(w, err)
			return
		}
		saved, err := editor.save(ctx, user.Name, input)
		switch {
		case errors.Is(err, profiles.ErrConflict):
			jsonReply(w, 409, map[string]string{"error": editor.title + " изменено в другой вкладке. Скопируйте свой текст и обновите страницу перед повторным сохранением."})
		case err != nil:
			var invalid *profiles.InputError
			if errors.As(err, &invalid) {
				profileInputError(w, err)
				return
			}
			jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить " + editor.name + "."})
		default:
			jsonReply(w, 200, editor.response(saved))
		}
	})
}
