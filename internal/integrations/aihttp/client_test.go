package aihttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type frame struct {
	Provider                         Provider
	Status, HTTPStatus               int
	Post                             bool
	Path, Response, Endpoint, Method string
	OK                               bool
	Code, Message                    string
	ProviderStatus                   int
}

func sourceCases(t *testing.T, raw []byte) {
	t.Helper()
	var rows []frame
	if e := json.Unmarshal(raw, &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) != 42 {
		t.Fatalf("source frames %d", len(rows))
	}
	for i, row := range rows {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c, e := New(row.Provider, "CI_SYNTHETIC_TOKEN")
			if e != nil {
				t.Fatal(e)
			}
			var calls int
			c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != row.Endpoint || r.Method != row.Method || r.Header.Get("Authorization") != "Bearer CI_SYNTHETIC_TOKEN" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatal("wire contract differs")
				}
				if r.GetBody != nil {
					t.Fatal("replayable POST")
				}
				return &http.Response{StatusCode: row.HTTPStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(row.Response)), Request: r}, nil
			})
			var body json.RawMessage
			if row.Post {
				body = json.RawMessage(`{"model":"synthetic-model"}`)
			}
			_, err := c.request(context.Background(), row.Path, body)
			if calls != 1 {
				t.Fatal("request retried")
			}
			if row.OK {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var got *Error
			if !errors.As(err, &got) {
				t.Fatalf("missing provider error: %v", err)
			}
			if got.Code != row.Code || got.Status != row.Status {
				t.Fatalf("source mapped error mismatch: %s %d, expected %s %d", got.Code, got.Status, row.Code, row.Status)
			}
			if got.Error() != row.Message || got.ProviderStatus != row.ProviderStatus {
				t.Fatalf("source error mismatch: %s / %s", got.Error(), row.Message)
			}
			if strings.Contains(fmt.Sprintf("%v %#v", err, c), "CI_PRIVATE_MARKER") || strings.Contains(fmt.Sprintf("%v %#v", c, c), "CI_SYNTHETIC_TOKEN") {
				t.Fatal("credential/diagnostic leaked")
			}
		})
	}
}
func TestSourceTransportContract(t *testing.T) {
	b, e := os.ReadFile("testdata/transport-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	sourceCases(t, b)
}
func TestCancellationTimeoutAndUncertainPost(t *testing.T) {
	for _, p := range []Provider{OpenAI, OpenRouter} {
		c, _ := New(p, "CI_SYNTHETIC_TOKEN")
		var calls atomic.Int64
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			<-r.Context().Done()
			return nil, fmt.Errorf("CI_PRIVATE_MARKER: %w", r.Context().Err())
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Models(ctx)
		if !errors.Is(err, context.Canceled) || calls.Load() != 0 || IsUncertain(err) {
			t.Fatal("pre-cancel sent request")
		}
		c.writeTimeout = time.Millisecond
		_, err = c.post(context.Background(), "responses", json.RawMessage(`{}`))
		var e *Error
		if !IsUncertain(err) || !errors.As(err, &e) || e.Code != "network" || calls.Load() != 1 {
			t.Fatal("uncertain timeout/retry contract")
		}
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) { cancel(); return nil, errors.New("CI_PRIVATE_MARKER") })
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		_, err = c.post(ctx, "responses", json.RawMessage(`{}`))
		if !errors.Is(err, context.Canceled) || !IsUncertain(err) {
			t.Fatal("inflight cancellation contract")
		}
	}
}
func TestRedirectAndBounds(t *testing.T) {
	for _, p := range []Provider{OpenAI, OpenRouter} {
		c, _ := New(p, "CI_SYNTHETIC_TOKEN")
		calls := 0
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://foreign.invalid/"}}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})
		_, err := c.Models(context.Background())
		if err == nil || calls != 1 {
			t.Fatal("redirect leaked authentication")
		}
		c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", maxResponseBytes+1)))}, nil
		})
		if _, err = c.Models(context.Background()); err == nil {
			t.Fatal("unbounded response")
		}
		calls = 0
		c.http.Transport = roundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not send") })
		for _, b := range []json.RawMessage{nil, []byte(`{`), []byte(strings.Repeat(" ", maxRequestBytes+1))} {
			if _, err = c.post(context.Background(), "responses", b); err == nil || IsUncertain(err) || calls != 0 {
				t.Fatal("invalid payload dispatched")
			}
		}
	}
}
func TestEndpointScopeAndKeyInput(t *testing.T) {
	for _, p := range []Provider{OpenAI, OpenRouter} {
		for _, key := range []string{"", "\nCI_SYNTHETIC_TOKEN", "  "} {
			if _, err := New(p, key); err == nil {
				t.Fatal("unsafe key accepted")
			}
		}
	}
	c, _ := New(OpenRouter, "CI_SYNTHETIC_TOKEN")
	if _, err := c.Responses(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("wrong endpoint")
	}
	c, _ = New(OpenAI, "CI_SYNTHETIC_TOKEN")
	if _, err := c.ChatCompletions(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("wrong endpoint")
	}
	if _, err := c.Key(context.Background()); err == nil {
		t.Fatal("wrong provider key endpoint")
	}
	if _, err := New("other", "CI_SYNTHETIC_TOKEN"); err == nil {
		t.Fatal("unknown provider")
	}
}
