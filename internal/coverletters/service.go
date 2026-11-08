package coverletters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/profiles"
)

type Runner interface {
	Automatic(context.Context,aiproviders.GenerationRequest,profiles.ModelInput,aiproviders.Billing)(aiproviders.AIResult,error)
}

// Progress получает только техническую стадию; сырые резюме/промт не передаём.
type Progress func(stage string)

type Service struct{
	Repository Repository
	Generator Runner
}

func (s Service) Generate(ctx context.Context,user string,id int64,regenerate bool,progress Progress)(Letter,error){
	if ctx.Err()!=nil{return Letter{},ctx.Err()}
	in,err:=s.Repository.Read(ctx,user,id)
	if err!=nil{return Letter{},err}
	if saved,ok:=in.Effective();ok&&!regenerate&&strings.TrimSpace(saved.CoverLetterMD)!=""{
		return saved,nil
	}
	if regenerate{
		saved,ok:=in.Effective()
		if !ok||strings.TrimSpace(saved.CoverLetterSubject)==""||strings.TrimSpace(saved.CoverLetterMD)==""{
			return Letter{},ErrNotRegenerable
		}
	}
	instructions,state,err:=messages(in.Prompt,in.Resume,in.Title,in.Description)
	if err!=nil{return Letter{},err}
	// Пока нет отдельного durable ledger для ручных ПЛАТНЫХ вызовов,
	// допускаем только OpenRouter с серверным max_price=0.
	// Выбранную платную модель никогда не заменяем молча бесплатной.
	if in.Config.Provider!="openrouter"||in.Config.OpenRouterModel!="best_free"{
		return Letter{},ErrPaidBilling
	}
	if s.Generator==nil{return Letter{},ErrUnavailable}
	if progress!=nil{progress("preparing")}
	lease,err:=s.Repository.Claim(ctx,user,in,regenerate)
	if errors.Is(err,ErrBusy){
		return s.wait(ctx,user,id,in,regenerate,progress)
	}
	if err!=nil{return Letter{},err}
	defer s.Repository.Release(lease)

	renewCtx,stop:=context.WithCancel(ctx)
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func(){
		defer heartbeat.Done()
		ticker:=time.NewTicker(20*time.Second)
		defer ticker.Stop()
		for{
			select{
			case <-renewCtx.Done():return
			case <-ticker.C:
				if s.Repository.Renew(renewCtx,lease)!=nil{return}
			}
		}
	}()
	defer func(){stop();heartbeat.Wait()}()
	request:=aiproviders.GenerationRequest{
		Task:"cover_letter",
		Messages:[]aiproviders.Message{{Role:"system",Content:instructions},{Role:"user",Content:state}},
		MaxTokens:4096,
		ValidateContent:func(raw string)error{
			_,e:=ParseAnswer(raw)
			return e
		},
		CheckActive:func(check context.Context)error{return s.Repository.Check(check,lease)},
	}
	if progress!=nil{progress("model")}
	result,err:=s.Generator.Automatic(ctx,request,in.Config,&freeBilling{repo:s.Repository,lease:lease})
	if err!=nil{return Letter{},err}
	answer,err:=ParseAnswer(result.Message.Content)
	if err!=nil{return Letter{},err}
	if progress!=nil{progress("saving")}
	letter,err:=s.Repository.Save(ctx,lease,in,answer,regenerate)
	if errors.Is(err,ErrChanged){
		// Только действительно заменённое другим запросом письмо может
		// считаться успехом. Изменение резюме/промта/модели не означает, что
		// устаревшая генерация завершилась успешно.
		current,e:=s.Repository.Read(ctx,user,id)
		if e==nil{
			if saved,ok:=advancedLetter(in,current);ok{return saved,nil}
		}
	}
	return letter,err
}


 // advancedLetter возвращает только письмо, которое действительно изменилось
 // после снимка генерации. Стабильное личное письмо при смене промта/резюме
 // остаётся сохранённым, но не выдаётся за результат нового запроса.
func advancedLetter(before,after Input)(Letter,bool){
	saved,ok:=after.Effective()
	if !ok||strings.TrimSpace(saved.CoverLetterMD)==""{return Letter{},false}
	switch saved.Source{
	case "personal":
		changed:=after.HasPersonal && (!before.HasPersonal ||
			after.PersonalRevision!=before.PersonalRevision ||
			after.PersonalSubject!=before.PersonalSubject ||
			after.PersonalMD!=before.PersonalMD ||
			after.PersonalOverride!=before.PersonalOverride)
		if !changed{return Letter{},false}
	case "vacancy":
		if before.SharedSubject==after.SharedSubject && before.SharedMD==after.SharedMD{
			return Letter{},false
		}
	default:
		return Letter{},false
	}
	return saved,true
}

// Если второй браузер открыл ту же вакансию, его запрос не запускает модель:
// ожидает уже сохранённую запись. После ошибки владельца не делает POST retry.
func (s Service) wait(ctx context.Context,user string,id int64,previous Input,regenerate bool,progress Progress)(Letter,error){
	if progress!=nil{progress("waiting")}
	ticker:=time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout:=time.NewTimer(2*time.Minute)
	defer timeout.Stop()
	for{
		select{
		case <-ctx.Done():return Letter{},ctx.Err()
		case <-timeout.C:return Letter{},ErrBusy
		case <-ticker.C:
			now,e:=s.Repository.Read(ctx,user,id)
			if e!=nil{return Letter{},e}
			if now.Hash()!=previous.Hash(){
				if saved,ok:=now.Effective();ok{
					if !regenerate||now.PersonalRevision!=previous.PersonalRevision||now.SharedMD!=previous.SharedMD{
						return saved,nil
					}
				}
				return Letter{},ErrChanged
			}
			var active int64
			e=s.Repository.DB.QueryRowContext(ctx,`SELECT COUNT(*) FROM cover_letter_generation_leases WHERE user_name=? AND vacancy_id=? AND expires_at>?`,
				user,id,time.Now().UnixMilli()).Scan(&active)
			if e!=nil{return Letter{},ErrUnavailable}
			if active==0{return Letter{},ErrBusy}
		}
	}
}

// freeBilling фиксирует каждую попытку модели до внешнего POST без содержимого
// резюме/вакансии. Нет таблицы с расходами ручного OpenAI: платный вызов
// запрещён, даже если caller передал настроенную модель.
type freeBilling struct{
	mu sync.Mutex
	repo Repository
	lease Lease
	count int
}

func (b *freeBilling) Run(ctx context.Context,request json.RawMessage,work func(context.Context)(json.RawMessage,error))(json.RawMessage,error){
	if b==nil||work==nil{return nil,ErrUnavailable}
	var meta struct{
		Provider string `json:"provider"`
		Model string `json:"model"`
		Free bool `json:"free"`
		Payload struct{
			Provider struct{MaxPrice map[string]float64 `json:"max_price"`} `json:"provider"`
		} `json:"payload"`
	}
	if json.Unmarshal(request,&meta)!=nil||meta.Provider!="openrouter"||!meta.Free||meta.Model==""||
		len(meta.Payload.Provider.MaxPrice)!=3||
		meta.Payload.Provider.MaxPrice["prompt"]!=0||
		meta.Payload.Provider.MaxPrice["completion"]!=0||
		meta.Payload.Provider.MaxPrice["request"]!=0{
		return nil,ErrPaidBilling
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.count++
	if b.count>8{return nil,ErrUnavailable}
	if err:=b.repo.Check(ctx,b.lease);err!=nil{return nil,err}
	if b.repo.DB==nil{return nil,ErrUnavailable}
	// Durable receipt записывается до POST. Если БД отказала, внешнего вызова нет.
	row,e:=b.repo.DB.ExecContext(ctx,`INSERT INTO openrouter_ai_logs(created_at,task,requested_primary_model,success,error_code)
	  VALUES(UTC_TIMESTAMP(),'cover_letter',?,0,'inflight')`,meta.Model)
	if e!=nil{return nil,ErrUnavailable}
	key,e:=row.LastInsertId()
	if e!=nil{return nil,ErrUnavailable}
	raw,callError:=work(ctx)
	finishCtx,cancel:=context.WithTimeout(context.Background(),5*time.Second)
	defer cancel()
	success,code:=0,"provider_error"
	if callError==nil{success,code=1,""}
	_,recordError:=b.repo.DB.ExecContext(finishCtx,`UPDATE openrouter_ai_logs SET success=?,error_code=? WHERE id=?`,success,code,key)
	if recordError!=nil{return nil,ErrUnavailable}
	return raw,callError
}
