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
	"unicode/utf8"

	"itmap/internal/aiproviders"
	"itmap/internal/auth"
	"itmap/internal/coverletters"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
)

// Интерфейс позволяет подменить весь генератор в синтетических HTTP-тестах.
type VacancyCoverLetterGenerator interface {
	Generate(context.Context,string,int64,bool,string,coverletters.Progress,...int)(coverletters.Letter,error)
}

func letterFailure(err error)(int,string){
	switch {
	case errors.Is(err,profiles.ErrResumeSelection):
		return 400,"Выбранное резюме недоступно."
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
	case errors.Is(err,coverletters.ErrPaidUncertain):
		return 409,"Для этих данных уже зарегистрирована платная попытка OpenAI. Повторный запрос заблокирован до проверки результата и расходов."
	case errors.Is(err,coverletters.ErrPaidBilling):
		return 503,"Платный OpenAI недоступен без безопасного учёта попыток. Проверьте модель и настройки."
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
		customPrompt, resumeID, status := readLetterPrompt(w, r, user.IsAdmin)
		if status != 0 {
			message := "Проверьте выбор резюме и пользовательский промт длиной до 5 000 символов."
			if status == http.StatusForbidden {
				message = "Пользовательский промт доступен администратору."
			}
			jsonReply(w, status, map[string]string{"error": message})
			return
		}
		if service==nil{jsonReply(w,503,map[string]string{"error":"Генератор писем не подключён."});return}
		// Server.WriteTimeout is an absolute 60s response deadline. Heartbeat
		// frames do not extend it, while letter generation can take 15 minutes.
		// Override only after session, origin and input validation. The request
		// context still cancels work on disconnect and after generationTimeout.
		const generationTimeout = 15 * time.Minute
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(generationTimeout + time.Minute))
		ctx,cancel:=context.WithTimeout(r.Context(),generationTimeout)
		defer cancel()
		if !strings.Contains(strings.ToLower(r.Header.Get("Accept")),"application/x-ndjson"){
			letter,err:=service.Generate(ctx,user.Name,id,regenerate,customPrompt,nil,resumeID)
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
		letter,err:=service.Generate(ctx,user.Name,id,regenerate,customPrompt,report,resumeID)
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

func readLetterPrompt(w http.ResponseWriter, r *http.Request, admin bool) (string, int, int) {
	if r.Body == nil {
		return "", 0, 0
	}
	// Учитываем до 12 байт на символ при JSON-экранировании суррогатной пары.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return "", 0, http.StatusRequestEntityTooLarge
		}
		return "", 0, http.StatusBadRequest
	}
	if len(body) == 0 {
		return "", 0, 0
	}
	var input struct {
		CustomPrompt *string `json:"customPrompt"`
		ResumeID *int `json:"resumeId"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if !utf8.Valid(body) || decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF ||
		(input.CustomPrompt == nil && input.ResumeID == nil) {
		return "", 0, http.StatusBadRequest
	}
	var prompt string
	var resumeID int
	if input.ResumeID!=nil {
		resumeID=*input.ResumeID
		if resumeID<1 || resumeID>3{return "",0,http.StatusBadRequest}
	}
	if input.CustomPrompt!=nil {
		if !admin{return "",0,http.StatusForbidden}
		if utf8.RuneCountInString(*input.CustomPrompt)>coverletters.MaxCustomPromptLength{return "",0,http.StatusBadRequest}
		prompt=strings.TrimSpace(*input.CustomPrompt)
	}
	return prompt,resumeID,0
}
