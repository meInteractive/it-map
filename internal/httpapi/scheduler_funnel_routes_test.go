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

type funnelHTTPProbe struct {
	calls int
	source,parser string
	fail error
}
func (p *funnelHTTPProbe) ReadFunnel(_ context.Context,source,parser string)(json.RawMessage,error){
	p.calls++
	p.source,p.parser=source,parser
	if p.fail!=nil {return nil,p.fail}
	return json.RawMessage(`{"checkedAt":1,"timezone":"Europe/Moscow","daily":[],"cohort":{"received":0}}`),nil
}

func TestSchedulerFunnelHTTPPermissionsFiltersAndFailure(t *testing.T){
	manager,err:=auth.NewManager(strings.Repeat("x",64),map[string]string{
		"eugene":"CI_pw","demo":"CI_pw","nastya":"CI_pw",
	})
	if err!=nil {t.Fatal(err)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","demo","nastya"}{
		user,_:=manager.Authenticate(login,"CI_pw")
		tokens[login],err=manager.Issue(user,time.Now())
		if err!=nil {t.Fatal(err)}
	}
	p:=&funnelHTTPProbe{}
	call:=func(method,login,path,origin string,store SchedulerFunnelReader)*httptest.ResponseRecorder{
		r:=httptest.NewRequest(method,"https://example.invalid"+path,strings.NewReader("{}"))
		if login!="" {r.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[login]})}
		if origin!="" {r.Header.Set("Origin",origin)}
		w:=httptest.NewRecorder()
		NewHandlerWithOptions(nil,Options{Auth:manager,SchedulerFunnel:store}).ServeHTTP(w,r)
		return w
	}
	for _,user:=range []string{"","demo","nastya"}{
		want:=403
		if user==""{want=401}
		if got:=call("GET",user,"/api/scheduler/funnel","",p).Code;got!=want{
			t.Fatalf("user %s: %d != %d",user,got,want)
		}
	}
	if p.calls!=0 {t.Fatal("unauthorized SQL access")}
	if got:=call("GET","eugene","/api/scheduler/funnel","",nil).Code;got!=503{t.Fatal("nil reader should fail closed",got)}
	for _,query:=range []string{"?parser=bad","?sourceId=bad","?parser=all&parser=all","?period=all","?sourceId="}{
		if got:=call("GET","eugene","/api/scheduler/funnel"+query,"",p).Code;got!=400{
			t.Fatalf("invalid filter %s: %d",query,got)
		}
	}
	if p.calls!=0 {t.Fatal("invalid filters reached storage")}
	w:=call("GET","eugene","/api/scheduler/funnel?sourceId=hh&parser=firecrawl","",p)
	if w.Code!=200 || p.calls!=1 || p.source!="hh" || p.parser!="firecrawl" ||
		w.Header().Get("Cache-Control")!="private, no-store" || !strings.Contains(w.Body.String(),"\"timezone\":\"Europe/Moscow\""){
		t.Fatal("Go funnel did not preserve admin / filter contract")
	}
	if w:=call("GET","eugene","/api/scheduler/funnel","",p); w.Code!=200 || p.source!="all" || p.parser!="all" {
		t.Fatal("default filters are not all")
	}
	if got:=call("POST","eugene","/api/scheduler/funnel","https://other.invalid",p).Code;got!=403{
		t.Fatal("cross-site write permitted")
	}
	if got:=call("POST","eugene","/api/scheduler/funnel","",p).Code;got!=405{
		t.Fatal("write permitted")
	}
	p.fail=errors.New("CI_PRIVATE_DATABASE_ERROR")
	w=call("GET","eugene","/api/scheduler/funnel","",p)
	if w.Code!=500 || strings.Contains(w.Body.String(),"CI_PRIVATE") || strings.Contains(w.Body.String(),"\"cohort\""){
		t.Fatal("storage failure leaked details or became fake zero")
	}
}
