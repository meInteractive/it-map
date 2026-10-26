package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type resumeProbe struct {
	reads, writes int
	user          string
	input         profiles.TextInput
	err           error
}

func (p *resumeProbe) ReadResume(_ context.Context, user string) (profiles.Resume, error) {
	p.reads++
	p.user = user
	return profiles.DefaultResume(), p.err
}
func (p *resumeProbe) SaveResume(_ context.Context, user string, input profiles.TextInput) (profiles.Resume, error) {
	p.writes++
	p.user = user
	p.input = input
	return profiles.Resume{ResumeMD: input.Text, Revision: input.Revision + 1, MaxLength: profiles.MaxResumeLength}, p.err
}
func TestProfileResumeRouteContract(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN_PASSWORD", "demo": "CI_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	user, _ := manager.Authenticate("demo", "CI_PASSWORD")
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	probe := &resumeProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, Resumes: probe})
	call := func(method, body string, logged bool, header http.Header, stream bool) *httptest.ResponseRecorder {
		var reader io.Reader = strings.NewReader(body)
		if stream {
			reader = struct{ io.Reader }{reader}
		}
		request := httptest.NewRequest(method, "https://itmap.test/api/profile/resume?userName=Eugene", reader)
		if logged {
			request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		}
		for key, values := range header {
			request.Header[key] = values
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if w := call("GET", "", false, nil, false); w.Code != 401 || probe.reads != 0 {
		t.Fatal("anonymous resume read allowed")
	}
	if w := call("POST", "{}", true, nil, false); w.Code != 405 || probe.writes != 0 {
		t.Fatal("unsupported resume method accepted")
	}
	if w := call("GET", "", true, nil, false); w.Code != 200 || probe.user != "Demo" || !strings.Contains(w.Body.String(), `"maxLength":20000`) || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("resume defaults/owner/cache contract changed")
	}
	for _, body := range []string{`{"resumeMd":"CI","revision":null}`, `{"resumeMd":"CI","revision":-1}`, `{"resumeMd":"CI","revision":0,"userName":"Eugene"}`, `{"ResumeMd":"CI","revision":0}`, `{"resumeMd":"CI\u0000","revision":0}`, `[]`, `{} {}`} {
		if w := call("PUT", body, true, nil, false); w.Code != 400 || probe.writes != 0 {
			t.Fatal("invalid resume input reached writer")
		}
	}
	if w := call("PUT", `{"resumeMd":"CI","revision":0}`, true, http.Header{"Origin": {"https://cross-site.invalid"}}, false); w.Code != 403 || probe.writes != 0 {
		t.Fatal("cross-origin resume write allowed")
	}
	// The source reader parses JSON independently of Content-Type.
	if w := call("PUT", `{"resumeMd":"  CI 😀 <&>  ","revision":0}`, true, http.Header{"Content-Type": {"text/plain"}, "Origin": {"https://itmap.test"}}, false); w.Code != 200 || probe.user != "Demo" || probe.input.Text != "  CI 😀 <&>  " {
		t.Fatal("resume write changed typed payload/owner or imposed a new MIME contract")
	}
	writes := probe.writes
	if w := call("PUT", strings.Repeat(" ", 256<<10)+`{}`, true, nil, true); w.Code != 413 || probe.writes != writes {
		t.Fatal("unbounded resume stream reached writer")
	}
	payload := `{"resumeMd":"CI boundary","revision":1}`
	payload = strings.Repeat(" ", (256<<10)-len(payload)) + payload
	if w := call("PUT", payload, true, nil, true); w.Code != 200 {
		t.Fatal("resume body at byte limit rejected")
	}
	probe.err = profiles.ErrConflict
	if w := call("PUT", `{"resumeMd":"CI","revision":0}`, true, nil, false); w.Code != 409 {
		t.Fatal("resume conflict did not return 409")
	}
	probe.err = errors.New("CI_PRIVATE_DATABASE_DETAILS")
	if w := call("GET", "", true, nil, false); w.Code != 503 || strings.Contains(w.Body.String(), "CI_PRIVATE_DATABASE_DETAILS") {
		t.Fatal("resume read exposed storage error")
	}
	if w := call("PUT", `{"resumeMd":"CI","revision":0}`, true, nil, false); w.Code != 503 || strings.Contains(w.Body.String(), "CI_PRIVATE_DATABASE_DETAILS") {
		t.Fatal("resume write exposed storage error")
	}
	unavailable := NewHandlerWithOptions(nil, Options{Auth: manager})
	for _, method := range []string{"GET", "POST"} {
		r := httptest.NewRequest(method, "https://itmap.test/api/profile/resume", nil)
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		w := httptest.NewRecorder()
		unavailable.ServeHTTP(w, r)
		want := 503
		if method == "POST" {
			want = 405
		}
		if w.Code != want {
			t.Fatal("resume dispatch order changed without storage")
		}
	}
}
