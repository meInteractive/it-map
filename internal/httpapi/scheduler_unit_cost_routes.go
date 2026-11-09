package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

type SchedulerUnitCostReader interface {
	ReadUnitCost(context.Context,int,string) (json.RawMessage,error)
}

// Admin-only SELECT endpoint. The ratio describes calendar-period local
// spending per first-created vacancy; it is NOT an acquisition-cohort KPI.
func SchedulerUnitCostHandler(m *auth.Manager, reader SchedulerUnitCostReader) http.HandlerFunc {
	return RequireSession(m,func(w http.ResponseWriter,r *http.Request){
		user,_:=sessionFromRequest(m,r)
		if !user.IsAdmin {
			jsonReply(w,403,map[string]string{"error":"Доступно только администратору."})
			return
		}
		if r.Method!=http.MethodGet {
			if !writeSameOrigin(r){
				jsonReply(w,403,map[string]string{"error":"Недопустимый источник запроса."})
				return
			}
			if _,ok:=schedulerRequestInput(w,r);ok{
				jsonReply(w,405,map[string]string{"error":"Метод не поддерживается."})
			}
			return
		}
		if r.URL.EscapedPath()!="/api/scheduler/unit-cost"{
			jsonReply(w,404,map[string]string{"error":"Маршрут не найден."})
			return
		}
		days,source:=30,"all"
		for key,values:=range r.URL.Query(){
			if len(values)!=1{
				jsonReply(w,400,map[string]string{"error":"Некорректные параметры статистики."})
				return
			}
			switch key{
			case "days":
				n,e:=strconv.Atoi(values[0])
				if e!=nil{jsonReply(w,400,map[string]string{"error":"Некорректный период."});return}
				days=n
			case "sourceId":
				source=values[0]
			default:
				jsonReply(w,400,map[string]string{"error":"Неизвестный параметр."})
				return
			}
		}
		if !parser.UnitCostFilterValid(days,source){
			jsonReply(w,400,map[string]string{"error":"Некорректный период или источник."})
			return
		}
		if reader==nil{
			jsonReply(w,503,map[string]string{"error":"Хранилище не подключено."})
			return
		}
		ctx,cancel:=context.WithTimeout(r.Context(),35*time.Second)
		defer cancel()
		body,err:=reader.ReadUnitCost(ctx,days,source)
		if err!=nil{
			status:=500
			if errors.Is(err,parser.ErrUnitCostInput){status=400}
			jsonReply(w,status,map[string]string{"error":"Не удалось получить статистику стоимости."})
			return
		}
		w.Header().Set("Cache-Control","private, no-store")
		jsonReply(w,200,body)
	})
}
