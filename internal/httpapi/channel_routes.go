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

	"itmap/internal/auth"
	"itmap/internal/channels"
)

type ChannelLister interface {
	List(context.Context) ([]channels.Channel, error)
}

type ChannelWriter interface {
	Create(context.Context, channels.Input) (channels.CreateResult, error)
	Update(context.Context, int64, channels.Input) (channels.UpdateResult, error)
	Delete(context.Context, int64) (channels.DeleteResult, error)
}

// The accepted GO-072 scope is a typed UI object. Read exact field spelling,
// preserve null/omitted fallback and ignore unrelated properties like source
// request.json. A primitive supplied for a text field is not a typed UI input.
func parseChannelInput(w http.ResponseWriter, r *http.Request) (channels.Input, error) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return channels.Input{}, errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	var fields map[string]json.RawMessage
	if e = d.Decode(&fields); e != nil {
		return channels.Input{}, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return channels.Input{}, errors.New("trailing JSON")
	}
	var input channels.Input
	for key, target := range map[string]**string{"name": &input.Name, "url": &input.URL, "username": &input.Username, "comment": &input.Comment, "lastSuccessfulCheckAt": &input.LastSuccessfulCheckAt} {
		if raw, ok := fields[key]; ok {
			if e = json.Unmarshal(raw, target); e != nil {
				return channels.Input{}, e
			}
		}
	}
	return input, nil
}
func channelWriteError(w http.ResponseWriter, err error, fallback string) {
	var validation *channels.ValidationError
	switch {
	case errors.Is(err, channels.ErrNotFound):
		jsonReply(w, 404, map[string]string{"error": "Канал не найден."})
	case errors.Is(err, channels.ErrSoleSource):
		jsonReply(w, 409, map[string]string{"error": channels.ErrSoleSource.Error()})
	case errors.As(err, &validation):
		jsonReply(w, 400, map[string]string{"error": validation.Message})
	default:
		jsonReply(w, 400, map[string]string{"error": fallback})
	}
}
func channelAdmin(m *auth.Manager, w http.ResponseWriter, r *http.Request) bool {
	u, ok := sessionFromRequest(m, r)
	if !ok {
		jsonReply(w, 401, map[string]string{"error": "Требуется авторизация."})
		return false
	}
	if !u.IsAdmin {
		jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
		return false
	}
	if !writeSameOrigin(r) {
		jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
		return false
	}
	return true
}
func channelUnavailable(w http.ResponseWriter) {
	jsonReply(w, 503, map[string]string{"error": "Хранилище каналов пока не подключено."})
}

func ChannelCollectionHandler(m *auth.Manager, lister ChannelLister, writer ChannelWriter) http.HandlerFunc {
	write := RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		if lister == nil && writer == nil {
			channelUnavailable(w)
			return
		}
		if r.Method != "POST" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if writer == nil {
			channelUnavailable(w)
			return
		}
		if !channelAdmin(m, w, r) {
			return
		}
		input, e := parseChannelInput(w, r)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		result, e := writer.Create(ctx, input)
		if e != nil {
			channelWriteError(w, e, "Не удалось добавить канал.")
			return
		}
		jsonReply(w, 201, result)
	})
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			if lister == nil {
				RequireSession(m, func(w http.ResponseWriter, _ *http.Request) { channelUnavailable(w) }).ServeHTTP(w, r)
				return
			}
			ChannelListHandler(m, lister).ServeHTTP(w, r)
			return
		}
		write.ServeHTTP(w, r)
	}
}
func ChannelItemHandler(m *auth.Manager, writer ChannelWriter) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		if writer == nil {
			channelUnavailable(w)
			return
		}
		if r.Method != "PUT" && r.Method != "DELETE" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if !channelAdmin(m, w, r) {
			return
		}
		text := r.PathValue("id")
		valid := text != ""
		for _, c := range text {
			if c < '0' || c > '9' {
				valid = false
			}
		}
		id, e := strconv.ParseInt(text, 10, 64)
		if e != nil || !valid {
			jsonReply(w, 404, map[string]string{"error": "Канал не найден."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
		defer cancel()
		if r.Method == "DELETE" {
			result, e := writer.Delete(ctx, id)
			if e != nil {
				channelWriteError(w, e, "Не удалось удалить канал.")
				return
			}
			jsonReply(w, 200, result)
			return
		}
		input, e := parseChannelInput(w, r)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		result, e := writer.Update(ctx, id, input)
		if e != nil {
			channelWriteError(w, e, "Не удалось обновить канал.")
			return
		}
		jsonReply(w, 200, result)
	})
}

func ChannelListHandler(m *auth.Manager, repo ChannelLister) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		items, err := repo.List(ctx)
		if err != nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]any{"error": "Хранилище каналов временно недоступно."})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"channels": items})
	})
}
