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

type hhScheduleProbe struct{ count int; fail error }
func (p *hhScheduleProbe) ReadHHScanSchedule(context.Context) (json.RawMessage,error) {
	p.count++
	if p.fail!=nil{return nil,p.fail}
	return json.RawMessage(`{"sourceId":"hh","enabled":true,"sourceSelected":true,"finalEligibilityVerified":false}`),nil
}
func TestGO290HHScanScheduleAdminReadOnly(t *testing.T){
	m,e:=auth.NewManager(strings.Repeat("k",64),map[string]string{"eugene":"ci_pass","demo":"ci_pass"})
	if e!=nil{t.Fatal(e)}
	sessions:=map[string]string{}
	for _,login:=range []string{"eugene","demo"}{
		u,_:=m.Authenticate(login,"ci_pass")
		sessions[login],e=m.Issue(u,time.Now())
		if e!=nil{t.Fatal(e)}
	}
	probe:=&hhScheduleProbe{}
	call:=func(method,user,path,origin string,reader SchedulerHHScanScheduleReader)*httptest.ResponseRecorder{
		r:=httptest.NewRequest(method,"https://example.invalid"+path,nil)
		if user!="" {r.AddCookie(&http.Cookie{Name:auth.CookieName,Value:sessions[user]})}
		if origin!=""{r.Header.Set("Origin",origin)}
		w:=httptest.NewRecorder()
		NewHandlerWithOptions(nil,Options{Auth:m,SchedulerHHScanSchedule:reader}).ServeHTTP(w,r)
		return w
	}
	if c:=call("GET","","/api/scheduler/hh-schedule","",probe).Code;c!=401{t.Fatal("anonymous",c)}
	if c:=call("GET","demo","/api/scheduler/hh-schedule","",probe).Code;c!=403{t.Fatal("nonadmin",c)}
	if probe.count!=0{t.Fatal("unauthorized read")}
	for _,bad:=range []string{"?days=7","?sourceId=hh","?x=1","?x=1&x=2"}{
		if c:=call("GET","eugene","/api/scheduler/hh-schedule"+bad,"",probe).Code;c!=400{t.Fatal("query accepted",bad,c)}
	}
	if c:=call("GET","eugene","/api/scheduler/hh-schedule","",nil).Code;c!=503{t.Fatal("missing reader",c)}
	if c:=call("POST","eugene","/api/scheduler/hh-schedule","https://evil.invalid",probe).Code;c!=403{t.Fatal("origin bypass",c)}
	if c:=call("POST","eugene","/api/scheduler/hh-schedule","",probe).Code;c!=405{t.Fatal("write accepted",c)}
	if probe.count!=0{t.Fatal("invalid input reached repository")}
	w:=call("GET","eugene","/api/scheduler/hh-schedule","",probe)
	if w.Code!=200||probe.count!=1||w.Header().Get("Cache-Control")!="private, no-store"{t.Fatal("GET contract",w.Code,probe.count)}
	probe.fail=errors.New("secret internal SQL")
	w=call("GET","eugene","/api/scheduler/hh-schedule","",probe)
	if w.Code!=500||strings.Contains(w.Body.String(),"secret"){t.Fatal("error details exposed")}
}
