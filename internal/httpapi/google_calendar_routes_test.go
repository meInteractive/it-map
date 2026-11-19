package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/integrations/googlecalendar"
)

func TestGoogleCalendarOAuthHTTPStateAndPermissions(t *testing.T) {
	m, err := auth.NewManager(strings.Repeat("S",64),map[string]string{"eugene":"CI_ADMIN_PASS","demo":"CI_PASSWORD"})
	if err!=nil {t.Fatal(err)}
	user,_:=m.Authenticate("demo","CI_PASSWORD")
	session,err:=m.Issue(user,time.Now())
	if err!=nil {t.Fatal(err)}
	cfg:=googlecalendar.Config{
		ClientID:"CI_CLIENT",ClientSecret:"CI_SECRET",
		RedirectURI:"https://example.invalid/api/google-calendar/callback",
		StateSecret:strings.Repeat("X",64),
	}
	h:=NewHandlerWithOptions(nil,Options{Auth:m,Calendar:&googlecalendar.Service{OAuth:cfg}})
	call:=func(method,path,origin string, sessionCookie, stateCookie *http.Cookie)*httptest.ResponseRecorder{
		req:=httptest.NewRequest(method,"https://example.invalid"+path,nil)
		if origin!="" {req.Header.Set("Origin",origin)}
		if sessionCookie!=nil{req.AddCookie(sessionCookie)}
		if stateCookie!=nil{req.AddCookie(stateCookie)}
		w:=httptest.NewRecorder()
		h.ServeHTTP(w,req)
		return w
	}
	login:=&http.Cookie{Name:auth.CookieName,Value:session}
	if w:=call(http.MethodGet,"/api/google-calendar/connect","",nil,nil);w.Code!=http.StatusUnauthorized{
		t.Fatal("unauthenticated OAuth start accepted",w.Code)
	}
	start:=call(http.MethodGet,"/api/google-calendar/connect","",login,nil)
	if start.Code!=http.StatusFound{t.Fatal("OAuth connect did not redirect",start.Code,start.Body.String())}
	redirect,err:=url.Parse(start.Header().Get("Location"))
	if err!=nil||redirect.Host!="accounts.google.com"||redirect.Query().Get("code_challenge_method")!="S256"{
		t.Fatal("untrusted redirect",err)
	}
	cookies:=start.Result().Cookies()
	if len(cookies)!=1||cookies[0].Name!=calendarStateCookie||!cookies[0].HttpOnly||!cookies[0].Secure||
		cookies[0].SameSite!=http.SameSiteLaxMode||cookies[0].Path!="/api/google-calendar/callback"{
		t.Fatal("wrong OAuth callback cookie")
	}
	state:=redirect.Query().Get("state")
	callback:="/api/google-calendar/callback?code=CI_CODE&state="+url.QueryEscape(state)
	if w:=call(http.MethodGet,callback,"",nil,nil);w.Code!=http.StatusForbidden {
		t.Fatal("state without cookie accepted",w.Code)
	}
	if w:=call(http.MethodGet,callback+"&state=CI_INVALID","",nil,cookies[0]);w.Code!=http.StatusForbidden {
		t.Fatal("duplicate callback state accepted",w.Code)
	}
	if w:=call(http.MethodGet,callback,"",nil,cookies[0]);w.Code!=http.StatusServiceUnavailable ||
		strings.Contains(w.Body.String(),"CI_CODE")||strings.Contains(w.Body.String(),"CI_SECRET") {
		t.Fatal("failed callback reflected secret or bypassed storage gate",w.Code,w.Body.String())
	}
	if w:=call(http.MethodPost,"/api/google-calendar/disconnect","https://evil.invalid",login,nil);w.Code!=http.StatusForbidden {
		t.Fatal("cross-origin disconnect accepted",w.Code)
	}
	if w:=call(http.MethodPost,"/api/google-calendar/sync","https://evil.invalid",login,nil);w.Code!=http.StatusForbidden {
		t.Fatal("cross-origin sync accepted",w.Code)
	}
	if w:=call(http.MethodPost,"/api/google-calendar/sync","",nil,nil);w.Code!=http.StatusUnauthorized {
		t.Fatal("unauthorized sync accepted",w.Code)
	}
	if w:=call(http.MethodPost,"/api/google-calendar/disconnect","",nil,nil);w.Code!=http.StatusUnauthorized {
		t.Fatal("unauthorized disconnect accepted",w.Code)
	}
}
