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

type repeatHTTPProbe struct {
	calls int
	days  int
	fail  error
}

func (p *repeatHTTPProbe) ReadExpenseRepeats(_ context.Context, days int) (json.RawMessage, error) {
	p.calls++
	p.days = days
	if p.fail != nil { return nil,p.fail }
	return json.RawMessage(`{"periodDays":30,"apify":{"repeatedReceiptGroups":0},"providerRepeatVerified":false}`),nil
}

func TestGO434ExpenseRepeatsHTTPAdminOnlyAndReadOnly(t *testing.T) {
	manager,err:=auth.NewManager(strings.Repeat("q",64),map[string]string{
		"eugene":"CI_pw","demo":"CI_pw","nastya":"CI_pw",
	})
	if err!=nil {t.Fatal(err)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","demo","nastya"}{
		user,_:=manager.Authenticate(login,"CI_pw")
		tokens[login],err=manager.Issue(user,time.Now())
		if err!=nil {t.Fatal(err)}
	}
	probe:=&repeatHTTPProbe{}
	call:=func(method,user,path,origin string,reader SchedulerExpenseRepeatsReader)*httptest.ResponseRecorder{
		r:=httptest.NewRequest(method,"https://example.invalid"+path,nil)
		if user!="" {r.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		if origin!="" {r.Header.Set("Origin",origin)}
		w:=httptest.NewRecorder()
		NewHandlerWithOptions(nil,Options{Auth:manager,SchedulerExpenseRepeats:reader}).ServeHTTP(w,r)
		return w
	}
	for _,user:=range []string{"","demo","nastya"}{
		expected:=http.StatusForbidden
		if user=="" {expected=http.StatusUnauthorized}
		if got:=call("GET",user,"/api/scheduler/expense-repeats","",probe).Code;got!=expected{
			t.Fatalf("access %q: %d, want %d",user,got,expected)
		}
	}
	if probe.calls!=0 {t.Fatal("unauthorized request reached ledger")}
	for _,query:=range []string{"?days=0","?days=8","?days=31","?days=x","?days=-7",
		"?days=7&days=30","?days=","?sourceId=hh","?days=7&sourceId=hh"}{
		if got:=call("GET","eugene","/api/scheduler/expense-repeats"+query,"",probe).Code;got!=400{
			t.Fatalf("bad query %q accepted: %d",query,got)
		}
	}
	if probe.calls!=0 {t.Fatal("invalid filters reached ledger")}
	if got:=call("GET","eugene","/api/scheduler/expense-repeats","",nil).Code;got!=503{
		t.Fatal("missing reader accepted",got)
	}
	if got:=call("POST","eugene","/api/scheduler/expense-repeats","https://evil.invalid",probe).Code;got!=403{
		t.Fatal("cross-origin mutation accepted",got)
	}
	if got:=call("POST","eugene","/api/scheduler/expense-repeats","",probe).Code;got!=405{
		t.Fatal("non-GET accepted",got)
	}
	if probe.calls!=0 {t.Fatal("write method reached reader")}
	w:=call("GET","eugene","/api/scheduler/expense-repeats?days=7","",probe)
	if w.Code!=200||probe.calls!=1||probe.days!=7||w.Header().Get("Cache-Control")!="private, no-store"{
		t.Fatal("safe read failed",w.Code,probe.calls,probe.days)
	}
	w=call("GET","eugene","/api/scheduler/expense-repeats","",probe)
	if w.Code!=200||probe.days!=30{t.Fatal("wrong default window")}
	probe.fail=errors.New("PRIVATE_ACTOR_TOKEN_AND_SQL")
	w=call("GET","eugene","/api/scheduler/expense-repeats","",probe)
	if w.Code!=500||strings.Contains(w.Body.String(),"PRIVATE_")||strings.Contains(w.Body.String(),"repeatedReceiptGroups"){
		t.Fatal("private ledger details leaked")
	}
}
