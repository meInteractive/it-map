//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
	f.exec("UPDATE prompt_model_settings SET provider='openai',openai_model='gpt-6-luna',revision=revision+1 WHERE kind='cover_letter'")
	_,err=svc.Generate(f.ctx,user,vacancyID,true,nil)
	if !errors.Is(err,coverletters.ErrPaidBilling)||mock.Calls!=3{
		t.Fatal("paid OpenAI call was not blocked",err,mock.Calls)
	}
	cached,err:=svc.Generate(f.ctx,user,vacancyID,false,nil)
	if err!=nil||cached.CoverLetterSubject!="CI New"{t.Fatal("existing text lost after paid model selection",err)}
	if strings.Contains(mock.Output,"PRIVATE"){t.Fatal("invalid mock fixture")}
	t.Log("PASS: saved/personal/regenerate, CAS input and provider, free-only billing, no OpenAI calls, read-only GET, one row, no PII in ledger")
}
