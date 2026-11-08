package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"itmap/internal/coverletters"
	"itmap/internal/integrations/aihttp"
)

// Интерфейс позволяет подменить весь генератор в синтетических HTTP-тестах.
type VacancyCoverLetterGenerator interface {
	Generate(context.Context,string,int64,bool,coverletters.Progress)(coverletters.Letter,error)
}

func letterFailure(err error)(int,string){
	switch {
	case errors.Is(err,coverletters.ErrMissing):
		return 404,"Вакансия не найдена."
	case errors.Is(err,coverletters.ErrNoResume):
		return 409,"Заполните резюме во вкладке «Резюме», чтобы создать письмо."
	case errors.Is(err,coverletters.ErrNoPrompt):
		return 409,"Промт сопроводительного письма пока не задан администратором."
	case errors.Is(err,coverletters.ErrNoVacancyText):
		return 409,"Для генерации нужны заголовок и полный текст вакансии."
	case errors.Is(err,coverletters.ErrNotRegenerable):
		return 409,"Повторная генерация доступна для письма с заголовком и текстом."
	case errors.Is(err,coverletters.ErrChanged):
		return 409,"Данные для письма изменились. Обновите карточку и повторите генерацию."
	case errors.Is(err,coverletters.ErrBusy):
		return 503,"Генерация письма занята. Повторите запрос после завершения."
	case errors.Is(err,coverletters.ErrPaidBilling):
		return 503,"Учёт платных запросов для писем ещё не подключён. Выберите бесплатную модель OpenRouter."
	case errors.Is(err,coverletters.ErrInvalid):
		return 502,"Модель не вернула письмо в корректном формате."
	}
	var provider *aihttp.Error
	var service *aiproviders.ServiceError
	if errors.As(err,&service){
		if service.Status>=400&&service.Status<=599{return service.Status,"Не удалось получить корректное письмо от модели."}
	}
	if errors.As(err,&provider){return 502,"Сервис модели временно недоступен."}
	if errors.Is(err,context.DeadlineExceeded){return 504,"Превышено время ожидания генерации письма."}
	return 503,"Не удалось создать письмо."
}

func VacancyCoverLetterHandler(m *auth.Manager,service VacancyCoverLetterGenerator,regenerate bool)http.HandlerFunc{
	return RequireSession(m,func(w http.ResponseWriter,r *http.Request){
		user,ok:=sessionFromRequest(m,r)
		if !ok{jsonReply(w,401,map[string]string{"error":"Требуется авторизация."});return}
		if !writeSameOrigin(r){jsonReply(w,403,map[string]string{"error":"Недопустимый источник запроса."});return}
		id,e:=strconv.ParseInt(r.PathValue("id"),10,64)
		if e!=nil||id<=0||id>9007199254740991{
			jsonReply(w,400,map[string]string{"error":"Некорректный ID вакансии."});return
		}
		if r.Body!=nil{
			body,err:=io.ReadAll(http.MaxBytesReader(w,r.Body,16))
			if err!=nil||len(body)>0{
				jsonReply(w,400,map[string]string{"error":"Генерация использует сохранённые данные; тело запроса не требуется."});return
			}
		}
		if service==nil{jsonReply(w,503,map[string]string{"error":"Генератор писем не подключён."});return}
		ctx,cancel:=context.WithTimeout(r.Context(),15*time.Minute)
		defer cancel()
		if !strings.Contains(strings.ToLower(r.Header.Get("Accept")),"application/x-ndjson"){
			letter,err:=service.Generate(ctx,user.Name,id,regenerate,nil)
			if err!=nil{
				status,message:=letterFailure(err)
				jsonReply(w,status,map[string]string{"error":message})
				return
			}
			jsonReply(w,200,letter)
			return
		}
		// React ждёт первый progress немедленно и затем heartbeat каждые 8 с.
		w.Header().Set("Content-Type","application/x-ndjson; charset=utf-8")
		w.Header().Set("Cache-Control","private, no-cache, no-transform")
		w.Header().Set("X-Accel-Buffering","no")
		w.WriteHeader(http.StatusOK)
		flusher,_:=w.(http.Flusher)
		var mu sync.Mutex
		start:=time.Now()
		stage:="preparing"
		send:=func(data map[string]any){
			mu.Lock()
			defer mu.Unlock()
			b,err:=json.Marshal(data)
			if err!=nil{return}
			b=append(b,'\n')
			_,_=w.Write(b)
			if flusher!=nil{flusher.Flush()}
		}
		report:=func(next string){
			mu.Lock();stage=next;mu.Unlock()
			send(map[string]any{"type":"progress","stage":next,"elapsedSeconds":int(time.Since(start).Seconds())})
		}
		report(stage)
		stopped:=make(chan struct{})
		var heartbeat sync.WaitGroup
		heartbeat.Add(1)
		go func(){
			defer heartbeat.Done()
			ticker:=time.NewTicker(8*time.Second)
			defer ticker.Stop()
			for {
				select{
				case <-ctx.Done():return
				case <-stopped:return
				case <-ticker.C:
					mu.Lock();current:=stage;mu.Unlock()
					send(map[string]any{"type":"progress","stage":current,"elapsedSeconds":int(time.Since(start).Seconds())})
				}
			}
		}()
		letter,err:=service.Generate(ctx,user.Name,id,regenerate,report)
		close(stopped)
		heartbeat.Wait()
		if err!=nil{
			_,message:=letterFailure(err)
			send(map[string]any{"type":"error","error":message})
			return
		}
		send(map[string]any{"type":"done","letter":letter})
	})
}
