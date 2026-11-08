//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/coverletters"
	"itmap/internal/profiles"
)

type letterMockGenerator struct{
	Calls int
	Output string
	Before func()
	After func()
}
func(g *letterMockGenerator)Automatic(ctx context.Context,request aiproviders.GenerationRequest,config profiles.ModelInput,billing aiproviders.Billing)(aiproviders.AIResult,error){
	g.Calls++
	if request.Task!="cover_letter"||config.Provider!="openrouter"||config.OpenRouterModel!="best_free"||len(request.Messages)!=2{
		return aiproviders.AIResult{},errors.New("invalid generated configuration")
	}
	if request.CheckActive==nil||request.CheckActive(ctx)!=nil{return aiproviders.AIResult{},errors.New("stale snapshot before request")}
	if g.Before!=nil{g.Before()}
	payload:=json.RawMessage(`{"provider":"openrouter","model":"ci/mock:free","free":true,"payload":{"provider":{"max_price":{"prompt":0,"completion":0,"request":0}}}}`)
	_,err:=billing.Run(ctx,payload,func(context.Context)(json.RawMessage,error){return json.RawMessage(`{"mock":true}`),nil})
	if err!=nil{return aiproviders.AIResult{},err}
	if request.ValidateContent!=nil{
		if err=request.ValidateContent(g.Output);err!=nil{return aiproviders.AIResult{},err}
	}
	// Хук симулирует запись в SQL между ответом провайдера и Save.
	if g.After!=nil{g.After()}
	return aiproviders.AIResult{Model:"ci/mock:free",Message:aiproviders.Message{Content:g.Output}},nil
}

func TestMariaDBCoverLetterGenerationDisposable(t *testing.T){
	requireSettingsCI(t)
	f:=openSettingsSQL(t)
	const vacancyID=190018
	const user="ci-letter-candidate"
	// Проверяем пустой namespace до операций с тестовыми user/vacancy IDs.
	var n int
	if err:=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM company_vacancies WHERE id=?",vacancyID).Scan(&n);err!=nil||n!=0{t.Fatal("CI letter vacancy already exists")}
	t.Cleanup(func(){
		for _,query:=range []string{
			"DELETE FROM cover_letter_generation_leases WHERE user_name='ci-letter-candidate'",
			"DELETE FROM vacancy_cover_letters WHERE user_name='ci-letter-candidate'",
			"DELETE FROM openrouter_ai_logs WHERE task='cover_letter' AND requested_primary_model='ci/mock:free'",
			"DELETE FROM user_profiles WHERE user_name='ci-letter-candidate'",
			"DELETE FROM system_prompts WHERE kind='cover_letter'",
			"DELETE FROM prompt_model_settings WHERE kind='cover_letter'",
			"DELETE FROM company_vacancies WHERE id=190018",
			"DELETE FROM company_crm WHERE company_name='CI Cover Letter Employer'",
		}{
			if _,err:=f.db.ExecContext(context.Background(),query);err!=nil{t.Error("CI letter cleanup failure",err)}
		}
	})
	f.exec("INSERT INTO company_crm(company_name) VALUES('CI Cover Letter Employer')")
	f.exec("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(190018,'CI Cover Letter Employer','https://example.invalid/cover/18','CI Head of Development','CI Vacancy requirements')")
	f.exec("INSERT INTO user_profiles(user_name,resume_md,revision) VALUES(?, 'CI Резюме руководителя',1)",user)
	f.exec("INSERT INTO system_prompts(kind,markdown,revision) VALUES('cover_letter','CI Пиши краткое письмо',1)")
	f.exec("INSERT INTO prompt_model_settings(kind,provider,openrouter_model,revision) VALUES('cover_letter','openrouter','best_free',1)")
	repo:=coverletters.Repository{DB:f.db}
	before:=dashboardFingerprint(f)
	input,err:=repo.Read(f.ctx,user,vacancyID)
	if err!=nil||input.Resume!="CI Резюме руководителя"||input.PromptRevision!=1{
		t.Fatal("source snapshot incorrect",err)
	}
	if before!=dashboardFingerprint(f){t.Fatal("read of cover letter mutated SQL")}
	mock:=&letterMockGenerator{Output:`{"subject":"CI Head","markdown":"CI персональный текст."}`}
	svc:=coverletters.Service{Repository:repo,Generator:mock}
	first,err:=svc.Generate(f.ctx,user,vacancyID,false,nil)
	if err!=nil||first.Source!="personal"||first.CoverLetterSubject!="CI Head"||mock.Calls!=1{
		t.Fatalf("initial generation failed: %+v %v calls=%d",first,err,mock.Calls)
	}
	second,err:=svc.Generate(f.ctx,user,vacancyID,false,nil)
	if err!=nil||second!=first||mock.Calls!=1{t.Fatal("saved letter was regenerated",err)}
	var rows,revision,override int
	if err=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*),MAX(revision),MAX(override_shared) FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?",user,vacancyID).Scan(&rows,&revision,&override);err!=nil||rows!=1||revision!=0||override!=0{
		t.Fatal("initial letter not stored exactly once",err,rows,revision,override)
	}
	mock.Output=`{"subject":"CI New","markdown":"CI Новое письмо."}`
	third,err:=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if err!=nil||third.CoverLetterSubject!="CI New"||mock.Calls!=2{
		t.Fatal("regenerate did not replace personal result",err)
	}
	if err=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*),MAX(revision),MAX(override_shared) FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?",user,vacancyID).Scan(&rows,&revision,&override);err!=nil||rows!=1||revision!=1||override!=1{
		t.Fatal("personal revision or override wrong",err,rows,revision,override)
	}
	// Изменение промта после отправки запрещает сохранение устаревшего результата.
	mock.Before=func(){f.exec("UPDATE system_prompts SET markdown='CI изменённый промт',revision=revision+1 WHERE kind='cover_letter'")}
	mock.Output=`{"subject":"CI Stale","markdown":"CI Устаревший текст."}`
	_,err=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if !errors.Is(err,coverletters.ErrChanged){t.Fatal("stale generated answer committed",err)}
	after,err:=repo.Read(f.ctx,user,vacancyID)
	if err!=nil||after.PersonalSubject!="CI New"{t.Fatal("stale result modified saved personal text",err)}
	var logs int
	if err=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='cover_letter' AND requested_primary_model='ci/mock:free' AND success=1").Scan(&logs);err!=nil||logs!=2{
		t.Fatal("free-provider usage ledger missing",err,logs)
	}
	// Если промт изменился ПОСЛЕ успешного mock provider result,
	// Save должен вернуть ErrChanged, а не старый текст как новый успех.
	mock.Before=nil
	mock.After=func(){f.exec("UPDATE system_prompts SET markdown='CI changed after provider',revision=revision+1 WHERE kind='cover_letter'")}
	mock.Output=`{"subject":"CI Uncommitted","markdown":"CI Не сохранено."}`
	_,err=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if !errors.Is(err,coverletters.ErrChanged){t.Fatal("stale prompt was misreported as successful regeneration",err)}
	stillSaved,err:=repo.Read(f.ctx,user,vacancyID)
	if err!=nil||stillSaved.PersonalSubject!="CI New"{t.Fatal("stale regeneration modified previous letter",err)}
	// Если другой запрос действительно сохранил новый персональный текст,
	// возвращаем победивший результат, не повторяя вызов модели.
	mock.After=func(){f.exec("UPDATE vacancy_cover_letters SET subject='CI Winner',markdown='CI Winner content',revision=revision+1 WHERE user_name=? AND vacancy_id=?",user,vacancyID)}
	mock.Output=`{"subject":"CI Lost Race","markdown":"CI Lost content."}`
	winner,err:=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if err!=nil||winner.CoverLetterSubject!="CI Winner"||winner.CoverLetterMD!="CI Winner content"{
		t.Fatal("newer personal letter not returned after concurrent update",winner,err)
	}
	mock.After=nil
	// Включение платной модели блокирует POST ДО модели, а старое письмо читается.
	callsBeforePaid:=mock.Calls
	var receiptsBeforePaid int
	if err=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='cover_letter' AND requested_primary_model='ci/mock:free'").Scan(&receiptsBeforePaid);err!=nil{t.Fatal("missing technical receipts before paid check",err)}
	f.exec("UPDATE prompt_model_settings SET provider='openai',openai_model='gpt-6-luna',revision=revision+1 WHERE kind='cover_letter'")
	_,err=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if !errors.Is(err,coverletters.ErrPaidBilling)||mock.Calls!=callsBeforePaid{
		t.Fatal("paid OpenAI call was not blocked",err,mock.Calls,callsBeforePaid)
	}
	var receiptsAfterPaid int
	if err=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM openrouter_ai_logs WHERE task='cover_letter' AND requested_primary_model='ci/mock:free'").Scan(&receiptsAfterPaid);err!=nil||receiptsAfterPaid!=receiptsBeforePaid{
		t.Fatal("paid model selection unexpectedly logged a provider attempt",err,receiptsBeforePaid,receiptsAfterPaid)
	}
	cached,err:=svc.Generate(f.ctx,user,vacancyID,false,nil)
	if err!=nil||cached!=winner{t.Fatal("winning personal letter lost after paid model selection",cached,winner,err)}

	// CI-only: Claim may lose a race after Read but before acquiring the
	// vacancy lock. An updated prompt must yield a conflict, not the old letter.
	f.exec("UPDATE prompt_model_settings SET provider='openrouter',openai_model='',openrouter_model='best_free',revision=revision+1 WHERE kind='cover_letter'")
	callsBeforeRace:=mock.Calls
	_,err=svc.Generate(f.ctx,user,vacancyID,true,func(stage string){
		if stage=="preparing"{
			f.exec("UPDATE system_prompts SET markdown='CI pre-claim update',revision=revision+1 WHERE kind='cover_letter'")
		}
	})
	if !errors.Is(err,coverletters.ErrChanged)||mock.Calls!=callsBeforeRace{
		t.Fatal("pre-claim prompt change reported as successful generation",err,mock.Calls,callsBeforeRace)
	}
	// However, an actually newer personal letter saved between Read and
	// Claim should be returned to the other tab without a provider request.
	var preclaimWinner coverletters.Letter
	preclaimWinner,err=svc.Generate(f.ctx,user,vacancyID,true,func(stage string){
		if stage=="preparing"{
			f.exec("UPDATE vacancy_cover_letters SET subject='CI Preclaim Winner',markdown='CI Newer preclaim letter',revision=revision+1 WHERE user_name=? AND vacancy_id=?",user,vacancyID)
		}
	})
	if err!=nil||preclaimWinner.CoverLetterSubject!="CI Preclaim Winner"||mock.Calls!=callsBeforeRace{
		t.Fatal("new pre-claim saved letter not returned",preclaimWinner,err,mock.Calls,callsBeforeRace)
	}
	// Two tabs: while the other request holds a lease, changing only a
	// shared manual letter must NOT return the unchanged personal override.
	currentInput,err:=repo.Read(f.ctx,user,vacancyID)
	if err!=nil||!currentInput.PersonalOverride{t.Fatal("missing effective personal override",err)}
	heldLease,err:=repo.Claim(f.ctx,user,currentInput,true)
	if err!=nil{t.Fatal("CI setup failed to hold owner lease",err)}
	waitCtx,stopWait:=context.WithTimeout(f.ctx,6*time.Second)
	waiting:=make(chan struct{},1)
	waitResult:=make(chan error,1)
	go func(){
		_,e:=svc.Generate(waitCtx,user,vacancyID,true,func(stage string){
			if stage=="waiting"{select{case waiting<-struct{}{}:default:}}
		})
		waitResult<-e
	}()
	select{
	case <-waiting:
	case <-time.After(3*time.Second):
		stopWait();repo.Release(heldLease);t.Fatal("second request did not enter waiting state")
	}
	f.exec("UPDATE company_vacancies SET cover_letter_subject='CI Shared',cover_letter_md='CI Updated shared letter' WHERE id=?",vacancyID)
	select{
	case e:=<-waitResult:
		if !errors.Is(e,coverletters.ErrChanged){t.Fatal("waiting request returned stale personal letter on shared-only update",e)}
	case <-time.After(4*time.Second):
		stopWait();repo.Release(heldLease);t.Fatal("waiting request failed to report changed input")
	}
	stopWait()
	repo.Release(heldLease)
	// Cancellation while waiting terminates promptly and never calls the
	// model. The owner lease remains fenced and is released explicitly.
	currentInput,err=repo.Read(f.ctx,user,vacancyID)
	if err!=nil{t.Fatal(err)}
	cancelLease,err:=repo.Claim(f.ctx,user,currentInput,true)
	if err!=nil{t.Fatal("CI setup failed to claim cancellation lease",err)}
	cancelCtx,cancelWaiting:=context.WithCancel(f.ctx)
	_,err=svc.Generate(cancelCtx,user,vacancyID,true,func(stage string){
		if stage=="waiting"{cancelWaiting()}
	})
	cancelWaiting()
	repo.Release(cancelLease)
	if !errors.Is(err,context.Canceled)||mock.Calls!=callsBeforeRace{
		t.Fatal("cancelled waiting request called provider or lost cancellation",err,mock.Calls,callsBeforeRace)
	}
	if strings.Contains(mock.Output,"PRIVATE"){t.Fatal("invalid mock fixture")}
	t.Log("PASS: saved/personal/regenerate, CAS input and provider, free-only billing, no OpenAI calls, read-only GET, one row, no PII in ledger")
}
