package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"itmap/internal/auth"
)

type SchedulerHHScanScheduleReader interface {
	ReadHHScanSchedule(context.Context) (json.RawMessage, error)
}

// Admin-only, GET-only observation. No scheduler claims or paid-provider calls.
func SchedulerHHScanScheduleHandler(manager *auth.Manager, reader SchedulerHHScanScheduleReader) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, _ := sessionFromRequest(manager, r)
		if !user.IsAdmin {
			jsonReply(w, http.StatusForbidden, map[string]string{"error":"Доступно только администратору."})
			return
		}
		if r.Method != http.MethodGet {
			if !writeSameOrigin(r) {
				jsonReply(w, http.StatusForbidden, map[string]string{"error":"Недопустимый источник запроса."})
				return
			}
			jsonReply(w, http.StatusMethodNotAllowed, map[string]string{"error":"Метод не поддерживается."})
			return
		}
		if r.URL.EscapedPath() != "/api/scheduler/hh-schedule" {
			jsonReply(w, http.StatusNotFound, map[string]string{"error":"Маршрут не найден."})
			return
		}
		if len(r.URL.Query()) != 0 {
			jsonReply(w, http.StatusBadRequest, map[string]string{"error":"Неизвестные параметры диагностики."})
			return
		}
		if reader == nil {
			jsonReply(w, http.StatusServiceUnavailable, map[string]string{"error":"Хранилище не подключено."})
			return
		}
		ctx,cancel:=context.WithTimeout(r.Context(),25*time.Second)
		defer cancel()
		body,err:=reader.ReadHHScanSchedule(ctx)
		if err!=nil {
			jsonReply(w,http.StatusInternalServerError,map[string]string{"error":"Не удалось прочитать расписание HH."})
			return
		}
		w.Header().Set("Cache-Control","private, no-store")
		jsonReply(w,http.StatusOK,body)
	})
}
