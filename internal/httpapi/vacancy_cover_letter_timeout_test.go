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
	"itmap/internal/coverletters"
)

type delayedCoverLetterGenerator struct {
	delay time.Duration
}

func (g delayedCoverLetterGenerator) Generate(ctx context.Context, _ string, _ int64, _ bool, progress coverletters.Progress) (coverletters.Letter, error) {
	if progress != nil {
		progress("model")
	}
	select {
	case <-time.After(g.delay):
	case <-ctx.Done():
		return coverletters.Letter{}, ctx.Err()
	}
	if progress != nil {
		progress("saving")
	}
	return coverletters.Letter{CoverLetterSubject: "CI Subject", CoverLetterMD: "CI Letter", Source: "personal"}, nil
}

func coverLetterDeadlineSession(t *testing.T) (*auth.Manager, string) {
	t.Helper()
	manager, err := auth.NewManager(strings.Repeat("d", 64), map[string]string{"demo": "CI_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	user, ok := manager.Authenticate("demo", "CI_PASSWORD")
	if !ok {
		t.Fatal("synthetic session failed")
	}
	token, err := manager.Issue(user, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return manager, token
}

// The real server has a 60s absolute WriteTimeout, while generation is allowed
// to run for 15 minutes. A short test deadline reproduces the same failure
// without sleeping for a minute or contacting a provider.
func TestCoverLetterResponseOutlivesServerWriteTimeout(t *testing.T) {
	manager, token := coverLetterDeadlineSession(t)
	handler := NewHandlerWithOptions(nil, Options{
		Auth: manager,
		VacancyCoverLetters: delayedCoverLetterGenerator{delay: 180 * time.Millisecond},
	})
	for _, accept := range []string{"application/json", "application/x-ndjson"} {
		t.Run(accept, func(t *testing.T) {
			server := httptest.NewUnstartedServer(handler)
			server.Config.WriteTimeout = 30 * time.Millisecond
			server.Start()
			defer server.Close()

			request, err := http.NewRequest(http.MethodPost, server.URL+"/api/vacancies/123/cover-letter", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
			request.Header.Set("Accept", accept)
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatalf("response cut off by server write deadline: %v", err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("response ended before successful generation: %v", err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status %d: %s", response.StatusCode, body)
			}
			if accept == "application/json" {
				var letter coverletters.Letter
				if err := json.Unmarshal(body, &letter); err != nil || letter.CoverLetterMD != "CI Letter" {
					t.Fatalf("invalid JSON letter: %v %s", err, body)
				}
			} else {
				frames := strings.Split(strings.TrimSpace(string(body)), "\n")
				if len(frames) < 3 {
					t.Fatalf("expected progress and final done: %s", body)
				}
				var last struct {
					Type string `json:"type"`
					Letter coverletters.Letter `json:"letter"`
				}
				if err := json.Unmarshal([]byte(frames[len(frames)-1]), &last); err != nil ||
					last.Type != "done" || last.Letter.CoverLetterMD != "CI Letter" {
					t.Fatalf("stream ended without saved letter: %v %s", err, body)
				}
			}
		})
	}
}

type canceledCoverLetterGenerator struct {
	started chan struct{}
	done chan struct{}
}

func (g canceledCoverLetterGenerator) Generate(ctx context.Context, _ string, _ int64, _ bool, _ coverletters.Progress) (coverletters.Letter, error) {
	close(g.started)
	<-ctx.Done()
	close(g.done)
	return coverletters.Letter{}, ctx.Err()
}

func TestCoverLetterStreamCancellationReachesGeneration(t *testing.T) {
	manager, token := coverLetterDeadlineSession(t)
	probe := canceledCoverLetterGenerator{started: make(chan struct{}), done: make(chan struct{})}
	server := httptest.NewServer(NewHandlerWithOptions(nil, Options{Auth: manager, VacancyCoverLetters: probe}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/vacancies/123/cover-letter", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
	request.Header.Set("Accept", "application/x-ndjson")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	select {
	case <-probe.started:
	case <-time.After(2 * time.Second):
		t.Fatal("generation never started")
	}
	cancel()
	select {
	case <-probe.done:
	case <-time.After(2 * time.Second):
		t.Fatal("generator did not observe client disconnect")
	}
}
