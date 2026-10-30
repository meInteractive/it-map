package aihttp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecisionsExactEndpointsAndNoReplay(t *testing.T) {
	c, _ := New(OpenRouter, "CI_SYNTHETIC_TOKEN")
	calls := 0
	c.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		expected := "https://openrouter.ai/api/v1/models?output_modalities=decisions"
		if r.Method == "POST" {
			expected = "https://openrouter.ai/api/alpha/decisions"
		}
		if r.URL.String() != expected || r.GetBody != nil {
			t.Fatal("wrong endpoint/replay")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: http.Header{}, Request: r}, nil
	})
	if _, e := c.DecisionModels(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Decisions(context.Background(), json.RawMessage(`{}`)); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal("retry")
	}
	a, _ := New(OpenAI, "CI_SYNTHETIC_TOKEN")
	if _, e := a.Decisions(context.Background(), []byte(`{}`)); e == nil {
		t.Fatal("wrong provider")
	}
}
