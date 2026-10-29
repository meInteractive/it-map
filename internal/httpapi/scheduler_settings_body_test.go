package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/parser"
)

// Проба связывает подготовку операции с поколением до первого Read тела.
type generationSettingsProbe struct {
	generation       string
	prepareError     error
	prepares, writes int
}

func (p *generationSettingsProbe) PrepareSave(_ context.Context, owner string) (func(context.Context, json.RawMessage) (json.RawMessage, error), error) {
	p.prepares++
	if p.prepareError != nil {
		return nil, p.prepareError
	}
	captured := p.generation
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		if owner != "Eugene" {
			panic("неверный authenticated owner")
		}
		if p.generation != captured {
			return nil, parser.ErrSettingsGeneration
		}
		p.writes++
		return json.RawMessage(`{"revision":8}`), nil
	}, nil
}

// Save нужен для проверки регрессии на обработчике до GO-328.
func (p *generationSettingsProbe) Save(ctx context.Context, body json.RawMessage, owner string) (json.RawMessage, error) {
	save, e := p.PrepareSave(ctx, owner)
	if e != nil {
		return nil, e
	}
	return save(ctx, body)
}

type settingsBodyRead struct {
	io.Reader
	before func()
	reads  int
}

func (b *settingsBodyRead) Read(p []byte) (int, error) {
	if b.reads == 0 {
		b.before()
	}
	b.reads++
	return b.Reader.Read(p)
}
func (b *settingsBodyRead) Close() error { return nil }

func TestSchedulerSettingsCapturesBeforeBody(t *testing.T) {
	m, e := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_pw"})
	if e != nil {
		t.Fatal(e)
	}
	u, _ := m.Authenticate("eugene", "CI_pw")
	token, e := m.Issue(u, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		name         string
		rotate, fail bool
		status       int
	}{
		{"stable", false, false, 200}, {"restore with same revision", true, false, 409}, {"capture failure before oversized body", false, true, 500},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := &generationSettingsProbe{generation: "old"}
			if c.fail {
				p.prepareError = parser.ErrSettingsStorage
			}
			body := &settingsBodyRead{Reader: strings.NewReader(`{"revision":7}`), before: func() {
				if c.rotate {
					p.generation = "new"
				}
			}}
			r := httptest.NewRequest("PUT", "https://example.invalid/api/scheduler/settings", nil)
			r.Body = body
			if c.fail {
				r.ContentLength = 32769
			}
			r.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			w := httptest.NewRecorder()
			SchedulerSettingsHandler(m, p).ServeHTTP(w, r)
			if w.Code != c.status {
				t.Fatalf("status=%d, ожидается=%d", w.Code, c.status)
			}
			if p.prepares != 1 {
				t.Fatal("поколение должно фиксироваться ровно один раз")
			}
			if c.fail && body.reads != 0 {
				t.Fatal("ошибка capture должна предшествовать чтению body")
			}
			if c.status != 200 && p.writes != 0 {
				t.Fatal("отклонённый запрос выполнил запись")
			}
			if c.status == 200 && p.writes != 1 {
				t.Fatal("обычный запрос не сохранил настройки")
			}
		})
	}
}
