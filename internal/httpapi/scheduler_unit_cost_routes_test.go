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
)

type unitCostProbe struct {
	calls int
	days int
	source string
	fail error
}
func (p *unitCostProbe) ReadUnitCost(_ context.Context,days int,source string)(json.RawMessage,error){
	p.calls++;p.days=days;p.source=source
	if p.fail!=nil{return nil,p.fail}
	return json.RawMessage(`{"periodDays":30,"sourceId":"all","newUniqueVacancies":0,"partialPeriodRatioUsd":null,"daily":[]}`),nil
}

func TestSchedulerUnitCostHTTPAccessAndFilters(t *testing.T){
	m,err:=auth.NewManager(strings.Repeat("u",64),map[string]string{"eugene":"CI_pw","demo":"CI_pw","nastya":"CI_pw"})
	if err!=nil{t.Fatal(err)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","demo","nastya"}{
		user,ok:=m.Authenticate(login,"CI_pw")
		if !ok{t.Fatal("CI auth setup failed")}
		tokens[login],err=m.Issue(user,time.Now())
		if err!=nil{t.Fatal(err)}
	}
	p:=&unitCostProbe{}
	call:=func(method,user,path,origin string,reader SchedulerUnitCostReader)*httptest.ResponseRecorder{
		r:=httptest.NewRequest(method,"https://example.invalid"+path,strings.NewReader("{}"))
		if user!=""{r.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		if origin!=""{r.Header.Set("Origin",origin)}
		w:=httptest.NewRecorder()
		NewHandlerWithOptions(nil,Options{Auth:m,SchedulerUnitCost:reader}).ServeHTTP(w,r)
		return w
	}
	for _,user:=range []string{"","demo","nastya"}{
		expected:=403
		if user==""{expected=401}
		if got:=call("GET",user,"/api/scheduler/unit-cost","",p).Code;got!=expected{t.Fatalf("user %s got %d, want %d",user,got,expected)}
	}
	if p.calls!=0{t.Fatal("unauthorized read reached SQL")}
	for _,query:=range []string{"?days=0","?days=8","?days=31","?days=-7","?days=x","?days=7&days=30","?sourceId=bad","?sourceId=","?period=30","?sourceId=hh&sourceId=hh"}{
		if got:=call("GET","eugene","/api/scheduler/unit-cost"+query,"",p).Code;got!=400{t.Fatalf("bad query %q was accepted: %d",query,got)}
	}
	if p.calls!=0{t.Fatal("invalid filters reached SQL")}
	if got:=call("GET","eugene","/api/scheduler/unit-cost","",nil).Code;got!=503{t.Fatal("missing reader accepted",got)}
	w:=call("GET","eugene","/api/scheduler/unit-cost?days=7&sourceId=hh","",p)
	if w.Code!=200||p.calls!=1||p.days!=7||p.source!="hh"||w.Header().Get("Cache-Control")!="private, no-store"{
		t.Fatal("admin cost read broke route/filter/cache policy",w.Code,p.calls,p.days,p.source)
	}
	w=call("GET","eugene","/api/scheduler/unit-cost","",p)
	if w.Code!=200||p.days!=30||p.source!="all"{t.Fatal("wrong default period")}
	if got:=call("POST","eugene","/api/scheduler/unit-cost","https://other.invalid",p).Code;got!=403{t.Fatal("cross-origin write accepted",got)}
	if got:=call("POST","eugene","/api/scheduler/unit-cost","",p).Code;got!=405{t.Fatal("write accepted",got)}
	p.fail=errors.New("CI_PRIVATE_LEDGER_DETAILS")
	w=call("GET","eugene","/api/scheduler/unit-cost","",p)
	if w.Code!=500||strings.Contains(w.Body.String(),"CI_PRIVATE")||strings.Contains(w.Body.String(),"newUniqueVacancies"){
		t.Fatal("private storage error leaked or became false zero")
	}
}

func TestSchedulerUnitCostUnavailableWhenNoDatabase(t *testing.T){
	if got:=SchedulerUnitCostHandler;got==nil{t.Fatal("handler missing")}
}
