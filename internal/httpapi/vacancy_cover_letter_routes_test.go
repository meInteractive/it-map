package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/coverletters"
)

type coverLetterProbe struct{
	Calls int
	User string
	Regenerate bool
	Err error
}
func(p *coverLetterProbe)Generate(_ context.Context,user string,id int64,regenerate bool,progress coverletters.Progress)(coverletters.Letter,error){
	p.Calls++;p.User=user;p.Regenerate=regenerate
	if id!=123{return coverletters.Letter{},coverletters.ErrMissing}
	if progress!=nil{progress("model");progress("saving")}
	if p.Err!=nil{return coverletters.Letter{},p.Err}
	return coverletters.Letter{CoverLetterSubject:"CI Тема",CoverLetterMD:"CI Текст",Source:"personal"},nil
}
func TestVacancyCoverLetterHTTPNoProviderCalls(t *testing.T){
	m,e:=auth.NewManager(strings.Repeat("h",64),map[string]string{"eugene":"CI_pw","nastya":"CI_pw","demo":"CI_pw"})
	if e!=nil{t.Fatal(e)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","nastya","demo"}{
		user,ok:=m.Authenticate(login,"CI_pw")
		if !ok{t.Fatal("login failed")}
		token,e:=m.Issue(user,time.Now());if e!=nil{t.Fatal(e)}
		tokens[login]=token
	}
	p:=&coverLetterProbe{}
	call:=func(user,method,path,origin,accept,body string)*httptest.ResponseRecorder{
		h:=NewHandlerWithOptions(nil,Options{Auth:m,VacancyCoverLetters:p})
		req:=httptest.NewRequest(method,"https://example.invalid"+path,strings.NewReader(body))
		if user!=""{req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		if origin!=""{req.Header.Set("Origin",origin)}
		if accept!=""{req.Header.Set("Accept",accept)}
		resp:=httptest.NewRecorder();h.ServeHTTP(resp,req);return resp
	}
	for _,tc:=range []struct{who,method,path,origin,accept,body string;status int}{
		{"","POST","/api/vacancies/123/cover-letter","","","",401},
		{"eugene","POST","/api/vacancies/123/cover-letter","https://evil.invalid","","",403},
		{"eugene","POST","/api/vacancies/not-number/cover-letter","","","",400},
		{"eugene","POST","/api/vacancies/0/cover-letter","","","",400},
		{"eugene","POST","/api/vacancies/123/cover-letter","","",`{"resume":"injected"}`,400},
		{"eugene","POST","/api/vacancies/123/cover-letter","","","x",400},
		{"eugene","POST","/api/vacancies/99/cover-letter","","","",404},
	}{
		w:=call(tc.who,tc.method,tc.path,tc.origin,tc.accept,tc.body)
		if w.Code!=tc.status{t.Errorf("%s %s: got %d want %d body=%s",tc.who,tc.path,w.Code,tc.status,w.Body.String())}
	}
	if p.Calls!=1{t.Fatal("invalid requests reached generation",p.Calls)}
	for _,user:=range []string{"eugene","nastya","demo"}{
		w:=call(user,"POST","/api/vacancies/123/cover-letter","","","")
		if w.Code!=200||p.User!=user{t.Fatalf("cross-user scope lost %s %d",user,w.Code)}
		var letter coverletters.Letter
		if json.Unmarshal(w.Body.Bytes(),&letter)!=nil||letter.Source!="personal"{t.Fatal("response contract",w.Body.String())}
	}
	w:=call("eugene","POST","/api/vacancies/123/cover-letter/regenerate","","application/x-ndjson","")
	if w.Code!=200||!p.Regenerate||!strings.Contains(w.Header().Get("Content-Type"),"application/x-ndjson"){t.Fatal("stream route failed",w.Code)}
	rows:=strings.Split(strings.TrimSpace(w.Body.String()),"\n")
	if len(rows)!=4{t.Fatalf("wrong stream events (%d): %s",len(rows),w.Body.String())}
	for _,line:=range rows{
		var event map[string]any
		if json.Unmarshal([]byte(line),&event)!=nil||event["type"]==nil{t.Fatal("invalid NDJSON",line)}
	}
	if !strings.Contains(rows[len(rows)-1],`"type":"done"`){t.Fatal("missing done frame")}
	p.Err=coverletters.ErrNoResume
	w=call("eugene","POST","/api/vacancies/123/cover-letter","","","")
	if w.Code!=409||!strings.Contains(w.Body.String(),"Резюме"){t.Fatal("wrong missing resume error",w.Code)}
	w=call("eugene","POST","/api/vacancies/123/cover-letter","","application/x-ndjson","")
	if w.Code!=200||!strings.Contains(w.Body.String(),`"type":"error"`){t.Fatal("stream lost error frame")}
	p.Err=errors.New("SECRET_DB_INTERNAL")
	w=call("eugene","POST","/api/vacancies/123/cover-letter","","","")
	if strings.Contains(w.Body.String(),"SECRET_DB_INTERNAL"){t.Fatal("raw backend error leaked")}
}
