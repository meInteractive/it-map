package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/integrations/googlecalendar"
)

const calendarStateCookie = "itmap_google_oauth_state"

func calendarClearState(w http.ResponseWriter) {
	http.SetCookie(w,&http.Cookie{Name:calendarStateCookie,Path:"/api/google-calendar/callback",HttpOnly:true,Secure:true,SameSite:http.SameSiteLaxMode,MaxAge:-1})
}

func GoogleCalendarStatusHandler(m *auth.Manager, svc *googlecalendar.Service) http.HandlerFunc {
	return RequireSession(m,func(w http.ResponseWriter,r *http.Request){
		user,ok:=sessionFromRequest(m,r)
		if !ok {jsonReply(w,http.StatusUnauthorized,map[string]string{"error":"Требуется авторизация."});return}
		ctx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
		defer cancel()
		out,e:=svc.Status(ctx,user.Name)
		if e!=nil {jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Статус Google Calendar временно недоступен."});return}
		w.Header().Set("Cache-Control","private, no-store")
		jsonReply(w,http.StatusOK,out)
	})
}

func GoogleCalendarConnectHandler(m *auth.Manager, svc *googlecalendar.Service) http.HandlerFunc {
	return RequireSession(m,func(w http.ResponseWriter,r *http.Request){
		user,ok:=sessionFromRequest(m,r)
		if !ok {jsonReply(w,http.StatusUnauthorized,map[string]string{"error":"Требуется авторизация."});return}
		if !svc.OAuth.Valid() {jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Google Calendar не настроен на сервере."});return}
		state,e:=svc.OAuth.NewState(user.Name,time.Now())
		if e!=nil {jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Не удалось подготовить подключение."});return}
		target,e:=svc.OAuth.AuthorizeURL(state)
		if e!=nil {jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Не удалось подготовить подключение."});return}
		http.SetCookie(w,&http.Cookie{Name:calendarStateCookie,Value:state,Path:"/api/google-calendar/callback",HttpOnly:true,Secure:true,SameSite:http.SameSiteLaxMode,MaxAge:int(googlecalendar.StateTTL.Seconds())})
		w.Header().Set("Cache-Control","private, no-store")
		http.Redirect(w,r,target,http.StatusFound)
	})
}

// OAuth callback не использует SameSite=Strict session cookie, отсутствующую
// при возврате из Google. Права выводятся только из проверенного HMAC state,
// совпадающего с cookie браузера. Секреты и code не попадают в URL redirect.
func GoogleCalendarCallbackHandler(svc *googlecalendar.Service) http.HandlerFunc {
	return func(w http.ResponseWriter,r *http.Request){
		cookie,e:=r.Cookie(calendarStateCookie)
		calendarClearState(w)
		w.Header().Set("Cache-Control","private, no-store")
		if e!=nil || r.URL.Query().Get("error")!="" {
			jsonReply(w,http.StatusBadRequest,map[string]string{"error":"Подключение Google Calendar отменено или устарело."});return
		}
		state:=r.URL.Query().Get("state")
		user,e:=svc.OAuth.VerifyState(state,cookie.Value,time.Now())
		if e!=nil {jsonReply(w,http.StatusForbidden,map[string]string{"error":"Недействительное подтверждение Google Calendar."});return}
		code:=r.URL.Query().Get("code")
		if code=="" || len(code)>4096 {jsonReply(w,http.StatusBadRequest,map[string]string{"error":"Google не передал код авторизации."});return}
		ctx,cancel:=context.WithTimeout(r.Context(),25*time.Second)
		defer cancel()
		e=svc.Connect(ctx,user,code,state)
		switch {
		case errors.Is(e,googlecalendar.ErrLinkedTask):
			jsonReply(w,http.StatusConflict,map[string]string{"error":"Найдено прежнее подключение задач: требуется безопасная сверка событий перед подключением другого аккаунта."})
		case e!=nil:
			jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Google Calendar не удалось подключить. Попробуйте ещё раз."})
		default:
			http.Redirect(w,r,"/?google-calendar=connected",http.StatusSeeOther)
		}
	}
}

func GoogleCalendarActionHandler(m *auth.Manager,svc *googlecalendar.Service,action string) http.HandlerFunc {
	return RequireSession(m,func(w http.ResponseWriter,r *http.Request){
		user,ok:=sessionFromRequest(m,r)
		if !ok {jsonReply(w,http.StatusUnauthorized,map[string]string{"error":"Требуется авторизация."});return}
		if !writeSameOrigin(r) {
			jsonReply(w,http.StatusForbidden,map[string]string{"error":"Недопустимый источник запроса."});return
		}
		ctx,cancel:=context.WithTimeout(r.Context(),55*time.Second)
		defer cancel()
		switch action {
		case "disconnect":
			if svc.Disconnect(ctx,user.Name)!=nil {
				jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Не удалось отключить Google Calendar."});return
			}
			jsonReply(w,http.StatusOK,map[string]any{"ok":true})
		case "sync":
			out,e:=svc.Sync(ctx,user.Name)
			switch {
			case errors.Is(e,googlecalendar.ErrDisconnected):
				jsonReply(w,http.StatusConflict,map[string]string{"error":"Подключите Google Calendar перед синхронизацией."})
			case errors.Is(e,googlecalendar.ErrConfiguration):
				jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Google Calendar не настроен на сервере."})
			case e!=nil:
				jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Не удалось синхронизировать задачи Google Calendar."})
			default:
				jsonReply(w,http.StatusOK,out)
			}
		default:
			http.NotFound(w,r)
		}
	})
}
