package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/auth"
	"itmap/internal/channels"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type channelWriteProbe struct {
	Calls   int
	Method  string
	ID      int64
	Input   channels.Input
	Failure error
}

func (p *channelWriteProbe) List(context.Context) ([]channels.Channel, error) {
	return []channels.Channel{}, nil
}
func (p *channelWriteProbe) Create(_ context.Context, in channels.Input) (channels.CreateResult, error) {
	p.Calls++
	p.Method = "POST"
	p.Input = in
	if p.Failure != nil {
		return channels.CreateResult{}, p.Failure
	}
	if _, e := channels.NormalizeInput(in, nil); e != nil {
		return channels.CreateResult{}, e
	}
	return channels.CreateResult{OK: true, ChannelID: 7, Channels: []channels.Channel{}}, nil
}
func (p *channelWriteProbe) Update(_ context.Context, id int64, in channels.Input) (channels.UpdateResult, error) {
	p.Calls++
	p.Method = "PUT"
	p.ID = id
	p.Input = in
	return channels.UpdateResult{OK: true, Channels: []channels.Channel{}}, p.Failure
}
func (p *channelWriteProbe) Delete(_ context.Context, id int64) (channels.DeleteResult, error) {
	p.Calls++
	p.Method = "DELETE"
	p.ID = id
	return channels.DeleteResult{OK: true, ChannelID: id, Name: "CI channel", DetachedVacancies: 3}, p.Failure
}
func channelWriteHTTP(t *testing.T) (http.Handler, *auth.Manager, *channelWriteProbe) {
	t.Helper()
	m, e := auth.NewManager(strings.Repeat("c", 64), map[string]string{"eugene": "CI password", "demo": "CI password"})
	if e != nil {
		t.Fatal(e)
	}
	p := &channelWriteProbe{}
	return NewHandlerWithOptions(nil, Options{Auth: m, Channels: p, ChannelWrites: p}), m, p
}
func channelRequest(t *testing.T, h http.Handler, m *auth.Manager, user, method, path, origin, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "https://itmap.test"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if user != "" {
		u, ok := m.Authenticate(user, "CI password")
		if !ok {
			t.Fatal("test account unavailable")
		}
		token, e := m.Issue(u, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestChannelWritesAuthAndOrigin(t *testing.T) {
	h, m, p := channelWriteHTTP(t)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		path := "/api/channels"
		if method != "POST" {
			path += "/7"
		}
		for _, v := range []struct {
			user, origin string
			status       int
		}{{"", "", 401}, {"demo", "", 403}, {"eugene", "https://other.invalid", 403}} {
			w := channelRequest(t, h, m, v.user, method, path, v.origin, `{"name":"CI","username":"@ci_channels"}`)
			if w.Code != v.status || p.Calls != 0 {
				t.Fatalf("channel auth/origin boundary failed: %s %d", method, w.Code)
			}
			if w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("private channel error cached")
			}
		}
	}
	for _, v := range []struct{ method, path string }{{"PATCH", "/api/channels"}, {"GET", "/api/channels/7"}, {"POST", "/api/channels/7"}} {
		w := channelRequest(t, h, m, "demo", v.method, v.path, "", `{}`)
		if w.Code != 405 || !strings.Contains(w.Body.String(), "Метод не поддерживается.") || p.Calls != 0 {
			t.Fatal("channel method contract changed")
		}
	}
}
func TestChannelWriteBodiesAndReplies(t *testing.T) {
	h, m, p := channelWriteHTTP(t)
	w := channelRequest(t, h, m, "eugene", "POST", "/api/channels", "https://itmap.test", `{"Name":"ignored","name":"bad","name":"CI good","username":"@ci_channels","comment":" CI ","ignored":true}`)
	if w.Code != 201 || p.Method != "POST" || p.Input.Name == nil || *p.Input.Name != "CI good" || p.Input.Comment == nil || *p.Input.Comment != " CI " {
		t.Fatal("typed channel payload or create status changed")
	}
	var created channels.CreateResult
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || created.ChannelID != 7 || created.Channels == nil {
		t.Fatal("create reply shape changed")
	}
	for _, body := range []string{`{`, `{} {}`, `{"comment":false}`, `{"url":5}`} {
		before := p.Calls
		w = channelRequest(t, h, m, "eugene", "PUT", "/api/channels/7", "", body)
		if w.Code != 400 || p.Calls != before {
			t.Fatal("malformed typed JSON reached repository")
		}
	}
	w = channelRequest(t, h, m, "eugene", "PUT", "/api/channels/0", "", `{"name":null,"comment":"","lastSuccessfulCheckAt":null}`)
	if w.Code != 200 || p.ID != 0 || p.Input.Name != nil || p.Input.Comment == nil || *p.Input.Comment != "" || p.Input.LastSuccessfulCheckAt != nil {
		t.Fatal("null/empty patch or zero ID changed")
	}
	w = channelRequest(t, h, m, "eugene", "DELETE", "/api/channels/7", "", `not JSON`)
	var deleted channels.DeleteResult
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &deleted) != nil || deleted.ChannelID != 7 || deleted.DetachedVacancies != 3 {
		t.Fatal("delete reply or ignored body changed")
	}
	for _, v := range []struct {
		err     error
		status  int
		message string
	}{{channels.ErrNotFound, 404, "Канал не найден."}, {channels.ErrSoleSource, 409, channels.ErrSoleSource.Error()}, {&channels.ValidationError{Message: "Такой независимый Telegram-канал уже добавлен."}, 400, "Такой независимый Telegram-канал уже добавлен."}, {errors.New("CI PRIVATE SQL details"), 400, "Не удалось удалить канал."}} {
		p.Failure = v.err
		w = channelRequest(t, h, m, "eugene", "DELETE", "/api/channels/7", "", "")
		var reply struct{ Error string }
		if w.Code != v.status || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Error != v.message || strings.Contains(w.Body.String(), "PRIVATE") {
			t.Fatal("channel error mapping or SQL privacy failed")
		}
	}
}
