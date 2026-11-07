package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

// SchedulerFunnelReader exposes only aggregated read-only statistics.
type SchedulerFunnelReader interface {
	ReadFunnel(context.Context,string,string) (json.RawMessage,error)
}

func SchedulerFunnelHandler(manager *auth.Manager,store SchedulerFunnelReader) http.HandlerFunc {
	return RequireSession(manager,func(w http.ResponseWriter,r *http.Request){
		user,_:=sessionFromRequest(manager,r)
		if !user.IsAdmin {
			jsonReply(w,http.StatusForbidden,map[string]string{"error":"Доступно только администратору."})
			return
		}
		if store==nil {
			jsonReply(w,http.StatusServiceUnavailable,map[string]string{"error":"Хранилище не подключено."})
			return
		}
		if r.Method!=http.MethodGet {
			if !writeSameOrigin(r) {
				jsonReply(w,http.StatusForbidden,map[string]string{"error":"Недопустимый источник запроса."})
				return
			}
			if _,ok:=schedulerRequestInput(w,r);ok{
				jsonReply(w,http.StatusMethodNotAllowed,map[string]string{"error":"Метод не поддерживается."})
			}
			return
		}
		if r.URL.EscapedPath()!="/api/scheduler/funnel"{
			jsonReply(w,http.StatusNotFound,map[string]string{"error":"Маршрут не найден."})
			return
		}
		values:=r.URL.Query()
		sourceID,parserID:="all","all"
		for name,list:=range values{
			if name!="sourceId" && name!="parser" || len(list)!=1{
				jsonReply(w,http.StatusBadRequest,map[string]string{"error":"Некорректные фильтры воронки."})
				return
			}
			if name=="sourceId"{sourceID=list[0]}else{parserID=list[0]}
		}
		if !parser.FunnelFilterValid(sourceID,parserID){
			jsonReply(w,http.StatusBadRequest,map[string]string{"error":"Некорректные фильтры воронки."})
			return
		}
		ctx,cancel:=context.WithTimeout(r.Context(),35*time.Second)
		defer cancel()
		body,err:=store.ReadFunnel(ctx,sourceID,parserID)
		if err!=nil {
			if errors.Is(err,parser.ErrFunnelInput) {
				jsonReply(w,http.StatusBadRequest,map[string]string{"error":"Некорректные фильтры воронки."})
			}else{
				jsonReply(w,http.StatusInternalServerError,map[string]string{"error":"Не удалось получить статистику воронки."})
			}
			return
		}
		jsonReply(w,http.StatusOK,body)
	})
}
