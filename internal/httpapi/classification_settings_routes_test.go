package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/classification"
)

type classificationSettingsProbe struct {
	reads int
	writes int
	err error
	rev int64
	prompt string
	enabled bool
}

func (p *classificationSettingsProbe) Read(_ context.Context) (classification.SettingsView,error) {
	p.reads++
	if p.err != nil { return classification.SettingsView{}, p.err }
	return classification.SettingsView{MaxLength:10000,Roles:[]classification.RoleSettings{
		{RoleID:1,Name:"CI root",Markdown:p.prompt,Enabled:p.enabled,Revision:p.rev,Descendants:[]classification.Descendant{{ID:2,Path:"CI root → CI child"}}},
	}},nil
}
func (p *classificationSettingsProbe) SaveEnabled(_ context.Context, id, revision int64, enabled bool) (classification.SavedRole,error) {
	p.writes++
	if p.err != nil { return classification.SavedRole{},p.err }
	if id != 1 || revision != p.rev { return classification.SavedRole{},classification.ErrConflict }
	p.rev++; p.enabled=enabled
	return classification.SavedRole{RoleID:1,Name:"CI root",Revision:p.rev,Markdown:p.prompt,Enabled:p.enabled,UpdatedAt:"2026-11-08T10:00:00Z"},nil
}
func (p *classificationSettingsProbe) SavePrompt(_ context.Context, id, revision int64, text string) (classification.SavedRole,error) {
	p.writes++
	if p.err != nil { return classification.SavedRole{},p.err }
	if id != 1 || revision != p.rev { return classification.SavedRole{},classification.ErrConflict }
	p.rev++;p.prompt=text
	return classification.SavedRole{RoleID:1,Name:"CI root",Revision:p.rev,Markdown:p.prompt,Enabled:p.enabled,UpdatedAt:"2026-11-08T10:00:00Z"},nil
}

func TestClassificationSettingsHTTPIsAdminOnlyAndDoesNotCallProviders(t *testing.T) {
	m,err:=auth.NewManager(strings.Repeat("z",64),map[string]string{"eugene":"CI_pw","nastya":"CI_pw","demo":"CI_pw"})
	if err!=nil {t.Fatal(err)}
	tokens:=map[string]string{}
	for _,login:=range []string{"eugene","nastya","demo"}{
		u,e:=m.Authenticate(login,"CI_pw");if e!=nil{t.Fatal(e)}
		token,e:=m.Issue(u,time.Now());if e!=nil{t.Fatal(e)}
		tokens[login]=token
	}
	store:=&classificationSettingsProbe{}
	call:=func(user,method,path,body,origin string,missing bool)*httptest.ResponseRecorder{
		var repo ClassificationSettingsStore=store
		if missing { repo=nil }
		h:=NewHandlerWithOptions(nil,Options{Auth:m,ClassificationSettings:repo})
		r:=httptest.NewRequest(method,"https://example.invalid"+path,strings.NewReader(body))
		if user!="" {r.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		if origin!="" {r.Header.Set("Origin",origin)}
		w:=httptest.NewRecorder();h.ServeHTTP(w,r);return w
	}
	for _,c:=range []struct{user,method,path,body,origin string;status int;missing bool}{
		{"","GET","/api/classification/settings","","",401,false},
		{"demo","GET","/api/classification/settings","","",403,false},
		{"nastya","PUT","/api/classification/roles/1",`{"enabled":true,"revision":0}`,"",403,false},
		{"eugene","GET","/api/classification/settings","","",200,false},
		{"eugene","GET","/api/classification/settings","","",503,true},
		{"eugene","POST","/api/classification/settings","","",405,false},
		{"eugene","GET","/api/classification/roles/1","","",405,false},
		{"eugene","GET","/api/classification/roles/2","","",405,false},
		{"eugene","POST","/api/classification/test","","",503,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":0}`,"https://other.invalid",403,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":null,"revision":0}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":1,"revision":0}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":null}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":-1}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":9007199254740991}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":0,"extra":1}`,"",400,false},
		{"eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":0} {}`,"",400,false},
		{"eugene","PUT","/api/classification/prompts/1",`{"markdown":null,"revision":0}`,"",400,false},
		{"eugene","PUT","/api/classification/prompts/1",`{"markdown":"hello","revision":"0"}`,"",400,false},
		{"eugene","PUT","/api/classification/prompts/1",`{"markdown":"hello","revision":0,"token":"never"}`,"",400,false},
		{"eugene","PUT","/api/classification/prompts/1",`{"markdown":"`+strings.Repeat("Я",10001)+`","revision":0}`,"",400,false},
		{"eugene","PUT","/api/classification/prompts/1",strings.Repeat("x",32769),"",413,false},
		{"eugene","PUT","/api/classification/prompts/not-an-id",`{}`,"",404,false},
		{"eugene","PUT","/api/classification/prompts/0",`{}`,"",404,false},
		{"eugene","PUT","/api/classification/prompts/1/extra",`{}`,"",404,false},
	} {
		w:=call(c.user,c.method,c.path,c.body,c.origin,c.missing)
		if w.Code!=c.status {t.Errorf("%s %s as %s: got %d want %d %s",c.method,c.path,c.user,w.Code,c.status,w.Body.String())}
	}
	if store.writes!=0 || store.reads!=1 {t.Fatalf("rejected attempts touched storage: reads=%d writes=%d",store.reads,store.writes)}
	enabled:=call("eugene","PUT","/api/classification/roles/1",`{"enabled":true,"revision":0}`,"",false)
	if enabled.Code!=200 || store.writes!=1 || !store.enabled || store.rev!=1 {t.Fatal("enabled CAS failed",enabled.Body.String())}
	stale:=call("eugene","PUT","/api/classification/roles/1",`{"enabled":false,"revision":0}`,"",false)
	if stale.Code!=409 || !store.enabled || store.rev!=1 {t.Fatal("stale revision overwrote enabled")}
	text:=call("eugene","PUT","/api/classification/prompts/1",`{"markdown":"Новый ✅","revision":1}`,"",false)
	if text.Code!=200 || store.prompt!="Новый ✅" || store.rev!=2 || !store.enabled {t.Fatal("prompt save lost state",text.Body.String())}
	notroot:=call("eugene","PUT","/api/classification/prompts/2",`{"markdown":"Нельзя","revision":0}`,"",false)
	if notroot.Code!=409 {t.Fatal("child prompt was accepted")}
	store.err=errors.New("CI_PRIVATE_SQL_PASSWORD should never reach response")
	private:=call("eugene","GET","/api/classification/settings","","",false)
	if private.Code!=503 || strings.Contains(private.Body.String(),"CI_PRIVATE") {t.Fatal("SQL details leaked",private.Body.String())}
}
