package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type HHRoleSettingsStore interface {
	Read(context.Context) (parser.HHRoleSettingsView, error)
	Save(context.Context, string, int64, int64, []string) (parser.HHRoleSetting, error)
}

func HHRoleSettingsHandler(manager *auth.Manager, store HHRoleSettingsStore, catalog HHProfessionalRolesReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, 403, map[string]string{"error": "Доступно только администратору."})
			return
		}
		if store == nil {
			jsonReply(w, 503, map[string]string{"error": "Настройки ролей HH недоступны."})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if r.Method == http.MethodGet {
			view, err := store.Read(ctx)
			if err != nil {
				jsonReply(w, 503, map[string]string{"error": "Настройки ролей HH недоступны."})
				return
			}
			jsonReply(w, 200, view)
			return
		}
		if r.Method != http.MethodPut {
			jsonReply(w, 405, map[string]string{"error": "Метод не поддерживается."})
			return
		}
		if !writeSameOrigin(r) {
			jsonReply(w, 403, map[string]string{"error": "Недопустимый источник запроса."})
			return
		}
		var request struct {
			Generation string   `json:"generation"`
			RoleID     int64    `json:"roleId"`
			Revision   int64    `json:"revision"`
			RoleIDs    []string `json:"roleIds"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		dec.DisallowUnknownFields()
		if dec.Decode(&request) != nil || dec.Decode(&struct{}{}) != io.EOF || request.RoleIDs == nil || len(request.RoleIDs) > 500 {
			jsonReply(w, 400, map[string]string{"error": "Некорректный список ролей."})
			return
		}
		if catalog == nil {
			jsonReply(w, 503, map[string]string{"error": "Справочник HH временно недоступен."})
			return
		}
		tree, err := catalog.FetchProfessionalRoles(ctx)
		if err != nil || tree == nil {
			jsonReply(w, 503, map[string]string{"error": "Справочник HH временно недоступен."})
			return
		}
		known := map[string]bool{}
		for _, category := range tree.Categories {
			for _, role := range category.Roles {
				known[role.ID] = true
			}
		}
		for _, id := range request.RoleIDs {
			if !known[id] {
				jsonReply(w, 400, map[string]string{"error": "Выбрана отсутствующая в справочнике роль HH."})
				return
			}
		}
		result, err := store.Save(ctx, request.Generation, request.RoleID, request.Revision, request.RoleIDs)
		if err != nil {
			switch {
			case errors.Is(err, parser.ErrSettingsChanged), errors.Is(err, parser.ErrSettingsGeneration):
				jsonReply(w, 409, map[string]string{"error": "Настройки изменились. Обновите страницу."})
			case errors.Is(err, parser.ErrHHRoleSettings):
				jsonReply(w, 400, map[string]string{"error": "Некорректный список ролей."})
			default:
				jsonReply(w, 503, map[string]string{"error": "Не удалось сохранить настройки ролей HH."})
			}
			return
		}
		jsonReply(w, 200, result)
	})
}
