package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"itmap/internal/auth"
	"itmap/internal/contacts"
	"mime"
	"net/http"
	"strconv"
	"time"
)

type HiringContactStore interface {
	List(context.Context, string) ([]contacts.Contact, error)
	Create(context.Context, string, contacts.Input) (contacts.Result, error)
	Update(context.Context, int64, contacts.Input) (contacts.Result, error)
	Delete(context.Context, int64) (contacts.Result, error)
}

func contactInput(w http.ResponseWriter, r *http.Request) (contacts.Input, error) {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return nil, errors.New("expected JSON")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := json.NewDecoder(r.Body)
	var in contacts.Input
	if e = d.Decode(&in); e != nil {
		return nil, e
	}
	var more any
	if e = d.Decode(&more); e != io.EOF {
		return nil, errors.New("trailing JSON")
	}
	return in, nil
}
func contactError(w http.ResponseWriter, e error, fallback string) {
	var validation *contacts.ValidationError
	switch {
	case errors.Is(e, contacts.ErrNotFound):
		jsonReply(w, 404, map[string]string{"error": "Контакт не найден."})
	case errors.Is(e, contacts.ErrCompanyNotFound):
		jsonReply(w, 404, map[string]string{"error": "Компания или агентство не найдены."})
	case errors.As(e, &validation):
		jsonReply(w, 400, map[string]string{"error": validation.Message})
	default:
		jsonReply(w, 400, map[string]string{"error": fallback})
	}
}
func contactUnavailable(w http.ResponseWriter) {
	jsonReply(w, 503, map[string]string{"error": "Хранилище контактов пока не подключено."})
}
func ContactCollectionHandler(m *auth.Manager, repo HiringContactStore) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			contactUnavailable(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if r.Method == "GET" {
			name := contacts.Trim(r.URL.Query().Get("company"))
			items, e := repo.List(ctx, name)
			if e != nil {
				if errors.Is(e, contacts.ErrCompanyNotFound) {
					contactError(w, e, "")
				} else {
					jsonReply(w, 503, map[string]string{"error": "Не удалось загрузить контакты."})
				}
				return
			}
			jsonReply(w, 200, map[string]any{"companyName": name, "limit": 3, "contacts": items})
			return
		}
		if r.Method != "POST" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if !legacyAdminWrite(m, w, r) {
			return
		}
		in, e := contactInput(w, r)
		if e != nil {
			jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
			return
		}
		name, e := contacts.OwnerName(in)
		if e != nil {
			contactError(w, e, "Не удалось добавить контакт.")
			return
		}
		result, e := repo.Create(ctx, name, in)
		if e != nil {
			contactError(w, e, "Не удалось добавить контакт.")
			return
		}
		jsonReply(w, 201, result)
	})
}
func ContactItemHandler(m *auth.Manager, repo HiringContactStore) http.HandlerFunc {
	return RequireSession(m, func(w http.ResponseWriter, r *http.Request) {
		if repo == nil {
			contactUnavailable(w)
			return
		}
		if !legacyAdminWrite(m, w, r) {
			return
		}
		if r.Method != "PUT" && r.Method != "DELETE" {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
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
			contactError(w, contacts.ErrNotFound, "")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var result contacts.Result
		if r.Method == "DELETE" {
			result, e = repo.Delete(ctx, id)
			if e != nil {
				contactError(w, e, "Не удалось удалить контакт.")
				return
			}
		} else {
			var in contacts.Input
			in, e = contactInput(w, r)
			if e != nil {
				jsonReply(w, 400, map[string]string{"error": "Некорректные данные."})
				return
			}
			result, e = repo.Update(ctx, id, in)
			if e != nil {
				contactError(w, e, "Не удалось обновить контакт.")
				return
			}
		}
		jsonReply(w, 200, result)
	})
}
