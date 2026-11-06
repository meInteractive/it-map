package hhpublic

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roleTransport func(*http.Request) (*http.Response, error)

func (f roleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchProfessionalRoles(t *testing.T) {
	valid := `{"categories":[{"id":"1","name":"ИТ","roles":[{"id":"107","name":"Руководитель проектов"}]},{"id":"2","name":"Другая","roles":[{"id":"107","name":"Руководитель проектов"}]}]}`
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		ok                      bool
	}{
		{"shared ID", valid, "application/json", 200, true},
		{"invalid JSON", "{", "application/json", 200, false},
		{"empty", `{"categories":[]}`, "application/json", 200, false},
		{"inconsistent ID", strings.Replace(valid, `"name":"Руководитель проектов"}`, `"name":"Иное"}`, 1), "application/json", 200, false},
		{"wrong type", valid, "text/html", 200, false},
		{"unavailable", valid, "application/json", 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &Client{http: &http.Client{Transport: roleTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.String() != "https://api.hh.ru/professional_roles" || req.Header.Get("HH-User-Agent") == "" {
					t.Fatalf("unexpected request %v", req.URL)
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			result, err := client.FetchProfessionalRoles(context.Background())
			if (err == nil) != tc.ok || (result != nil) != tc.ok {
				t.Fatalf("result=%v error=%v", result, err)
			}
		})
	}
}
