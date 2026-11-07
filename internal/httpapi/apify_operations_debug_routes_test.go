package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/auth"
)

type apifyOperationsHTTPProbe struct {
	calls int
	err error
}

func (p *apifyOperationsHTTPProbe) ReadOperationsDebug(context.Context) (apifyruns.OperationsDebugView, error) {
	p.calls++
	if p.err != nil { return apifyruns.OperationsDebugView{},p.err }
	return apifyruns.OperationsDebugView{Items:[]apifyruns.OperationDebugItem{},Total:0,Limit:10,Coverage:"server_latest"},nil
}

func TestApifyOperationsDebugAdminReadOnly(t *testing.T) {
	manager,err:=auth.NewManager(strings.Repeat("x",64),map[string]string{
		"eugene":"CI_pw","nastya":"CI_pw","demo":"CI_pw",
	})
	if err!=nil{t.Fatal(err)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","nastya","demo"}{
		user,_:=manager.Authenticate(login,"CI_pw")
		tokens[login],err=manager.Issue(user,time.Now())
		if err!=nil{t.Fatal(err)}
	}
	probe:=&apifyOperationsHTTPProbe{}
	call:=func(method,user,path string,store ApifyOperationsDebugReader)*httptest.ResponseRecorder{
		req:=httptest.NewRequest(method,"https://example.invalid"+path,nil)
		if user!="" {req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		w:=httptest.NewRecorder()
		ApifyOperationsDebugHandler(manager,store)(w,req)
		return w
	}
	for _,user:=range []string{"","demo","nastya"}{
		expected:=403
		if user=="" {expected=401}
		if w:=call("GET",user,"/api/apify/operations/debug",probe);w.Code!=expected{
			t.Fatalf("%s status %d want %d",user,w.Code,expected)
		}
	}
	if probe.calls!=0{t.Fatal("unauthorized read reached database")}
	if w:=call("POST","eugene","/api/apify/operations/debug",probe);w.Code!=405{t.Fatal("POST accepted")}
	if w:=call("GET","eugene","/api/apify/operations/debug?limit=100",probe);w.Code!=400{t.Fatal("unreviewed query accepted")}
	if probe.calls!=0{t.Fatal("invalid request reached database")}
	if w:=call("GET","eugene","/api/apify/operations/debug",nil);w.Code!=503{t.Fatal("nil store succeeded")}
	w:=call("GET","eugene","/api/apify/operations/debug",probe)
	if w.Code!=200 || probe.calls!=1 || w.Header().Get("Cache-Control")!="private, no-store"||
		!strings.Contains(w.Body.String(),"\"coverage\":\"server_latest\""){
		t.Fatal("valid admin GET failed",w.Code,w.Body.String())
	}
	probe.err=errors.New("CI_PRIVATE_DATABASE_PASSWORD")
	w=call("GET","eugene","/api/apify/operations/debug",probe)
	if w.Code!=503 || strings.Contains(w.Body.String(),"CI_PRIVATE") {
		t.Fatal("storage failure leaked",w.Body.String())
	}
}
