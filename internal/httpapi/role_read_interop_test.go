//go:build legacyinterop

package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/roles"
)

type roleReadSourceProbe struct {
	rows  []roles.Role
	calls int
}

func (p *roleReadSourceProbe) Read(context.Context) (roles.Catalog, error) {
	p.calls++
	return roles.Build(p.rows)
}

func TestRoleReadHTTPSource(t *testing.T) {
	m, err := auth.NewManager("CI_SYNTHETIC_ROLE_READ_SECRET_341", map[string]string{"eugene": "CI_pw", "nastya": "CI_pw", "demo": "CI_pw"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, login := range []string{"eugene", "nastya", "demo"} {
		u, _ := m.Authenticate(login, "CI_pw")
		token, err := m.Issue(u, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		tokens[u.Name] = token
	}
	cmd := exec.Command("node", "../../scripts/check-role-read-http.mjs")
	cmd.Stderr = os.Stderr
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	decoder, count := json.NewDecoder(pipe), 0
	for {
		var frame struct {
			Name, Path, User, Origin, ResponseBody string
			NoDB, BadCookie                        bool
			Reads, Status                          int
			Rows                                   []roles.Role
		}
		err = decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(frame.Name, func(t *testing.T) {
			probe := &roleReadSourceProbe{rows: frame.Rows}
			var repo RoleReader = probe
			if frame.NoDB {
				repo = nil
			}
			handler := NewHandlerWithOptions(nil, Options{Auth: m, Roles: repo})
			req := httptest.NewRequest("GET", "https://example.invalid"+frame.Path, nil)
			if frame.Origin != "" {
				req.Header.Set("Origin", frame.Origin)
			}
			if frame.User != "" {
				token := tokens[frame.User]
				if frame.BadCookie {
					token = "CI_INVALID_COOKIE"
				}
				req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			}
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, req)
			if out.Code != frame.Status {
				t.Fatalf("source status differs: got %d want %d", out.Code, frame.Status)
			}
			var got, want any
			if json.Valid([]byte(frame.ResponseBody)) {
				if json.Unmarshal(out.Body.Bytes(), &got) != nil || json.Unmarshal([]byte(frame.ResponseBody), &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("source JSON differs: got %s want %s", out.Body.String(), frame.ResponseBody)
				}
			} else if strings.TrimSuffix(out.Body.String(), "\n") != frame.ResponseBody {
				t.Fatalf("source text differs: %q", out.Body.String())
			}
			if probe.calls != frame.Reads {
				t.Fatalf("source authorization/path/storage precedence differs: reads %d want %d", probe.calls, frame.Reads)
			}
		})
	}
	if cmd.Wait() != nil || count != 166 {
		t.Fatal("incomplete role GET corpus", count)
	}
}
