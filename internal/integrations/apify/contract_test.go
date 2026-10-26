package apify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const syntheticToken = "CI_SYNTHETIC_NOT_AN_API_KEY"

type transportVector struct {
	Name, Path string
	Options    struct {
		Method   string
		Body     json.RawMessage
		MaxBytes int64
		Text     bool
	}
	Response struct {
		Status     int
		Body, Mode string
	}
	Valid         bool
	Value         json.RawMessage
	Code, Message string
	Status, Calls int
	Request       *struct {
		URL, Method, Redirect, ContentType string
		Body                               *string
		AuthOK                             bool
	}
}
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func mockClient(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int64) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client, err := New(syntheticToken)
	if err != nil {
		t.Fatal(err)
	}
	inner := &http.Transport{Proxy: nil}
	t.Cleanup(inner.CloseIdleConnections)
	calls := &atomic.Int64{}
	client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "api.apify.com" {
			return nil, errors.New("CI refused external mock destination")
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		copy.URL = &u
		copy.Host = ""
		return inner.RoundTrip(copy)
	})
	return client, calls
}
func TestApifyLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/transport-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Actor                     string
		RunCapUSD, ReadReserveUSD float64
		HH                        []struct {
			Name, URL     string
			Options       HHOptions
			Valid         bool
			Normalized    StartInput
			Code, Message string
			Status        int
		}
		Transport []transportVector
		Runs      []struct {
			Input       json.RawMessage
			Valid       bool
			State, Code string
			Status      int
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Actor != Actor || fixture.RunCapUSD != RunCapUSD || fixture.ReadReserveUSD != ReadReserveUSD || len(fixture.HH) < 30 || len(fixture.Transport) < 35 || len(fixture.Runs) != 14 {
		t.Fatal("legacy Apify vectors/constants missing")
	}
	for _, v := range fixture.HH {
		t.Run("HH/"+v.Name, func(t *testing.T) {
			got, err := HHInput(v.URL, v.Options)
			if !v.Valid {
				assertError(t, err, v.Code, v.Status, v.Message)
				return
			}
			if err != nil {
				t.Fatal("valid legacy HH source refused")
			}
			if !reflect.DeepEqual(got, v.Normalized) {
				t.Fatalf("HH normalization differs for %s", v.Name)
			}
		})
	}
	for _, v := range fixture.Transport {
		t.Run("HTTP/"+v.Name, func(t *testing.T) {
			type seen struct{ path, method, auth, contentType, body string }
			observed := make(chan seen, 1)
			client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				observed <- seen{r.URL.RequestURI(), r.Method, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)}
				if v.Response.Mode == "network" || v.Response.Mode == "stream-error" {
					conn, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer conn.Close()
					if v.Response.Mode == "stream-error" {
						fmt.Fprint(buffer, "HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\n{")
						buffer.Flush()
					}
					return
				}
				if v.Response.Status == 302 {
					w.Header().Set("Location", "https://ci.invalid/")
				}
				w.WriteHeader(v.Response.Status)
				if v.Response.Mode != "empty" {
					io.WriteString(w, v.Response.Body)
				}
			})
			var compact bytes.Buffer
			if len(v.Options.Body) > 0 {
				if json.Compact(&compact, v.Options.Body) != nil {
					t.Fatal("bad fixture body")
				}
			}
			got, _, err := client.request(context.Background(), v.Path, requestOptions{method: v.Options.Method, body: compact.Bytes(), maxBytes: v.Options.MaxBytes, text: v.Options.Text})
			if calls.Load() != int64(v.Calls) {
				t.Fatal("request count differs; redirect/retry/scope regression")
			}
			if !v.Valid {
				assertError(t, err, v.Code, v.Status, v.Message)
			} else if err != nil {
				t.Fatal("valid legacy mock response refused")
			} else if v.Options.Text {
				var expected string
				if json.Unmarshal(v.Value, &expected) != nil || string(got) != expected {
					t.Fatal("text response differs")
				}
			} else {
				var a, b any
				if json.Unmarshal(got, &a) != nil || json.Unmarshal(v.Value, &b) != nil || !reflect.DeepEqual(a, b) {
					t.Fatal("JSON response differs")
				}
			}
			if v.Request != nil {
				select {
				case r := <-observed:
					if "https://api.apify.com"+r.path != v.Request.URL || r.method != v.Request.Method || r.auth != "Bearer "+syntheticToken || r.contentType != v.Request.ContentType || !v.Request.AuthOK || v.Request.Redirect != "manual" {
						t.Fatal("request metadata differs")
					}
					wantBody := ""
					if v.Request.Body != nil {
						wantBody = *v.Request.Body
					}
					if r.body != wantBody {
						t.Fatal("request body differs")
					}
				case <-time.After(time.Second):
					t.Fatal("mock request not observed")
				}
			}
			if strings.Contains(fmt.Sprintf("%v %#v %v", client, client, err), syntheticToken) {
				t.Fatal("credential appeared in debug/error output")
			}
		})
	}
	for i, v := range fixture.Runs {
		t.Run(fmt.Sprintf("Run/%d", i), func(t *testing.T) {
			raw := append([]byte(`{"data":`), v.Input...)
			raw = append(raw, '}')
			got, err := parseRun(raw)
			if !v.Valid {
				assertError(t, err, v.Code, v.Status, "")
				return
			}
			if err != nil {
				t.Fatal("valid legacy run refused")
			}
			state, err := got.State()
			if err != nil || state != v.State {
				t.Fatal("run state differs")
			}
			var a, b any
			json.Unmarshal(v.Input, &a)
			json.Unmarshal(got.Raw, &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("run metadata was discarded")
			}
		})
	}
}
func TestApifyDatasetLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/transport-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Datasets []struct {
			Name, Kind, ID string
			Offset         int64
			Clean          bool
			Body           json.RawMessage
			Valid          bool
			Value          json.RawMessage
			Code, Message  string
			Status, Calls  int
			RequestURL     *string
		}
	}
	if json.Unmarshal(data, &fixture) != nil || len(fixture.Datasets) != 18 {
		t.Fatal("required dataset fixtures missing")
	}
	for _, v := range fixture.Datasets {
		t.Run(v.Name, func(t *testing.T) {
			observed := make(chan string, 1)
			client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				observed <- "https://api.apify.com" + r.URL.RequestURI()
				w.Header().Set("Content-Type", "application/json")
				w.Write(v.Body)
			})
			var got any
			var err error
			if v.Kind == "items" {
				got, err = client.DatasetItems(context.Background(), v.ID, v.Offset, v.Clean)
			} else {
				got, err = client.DatasetInfo(context.Background(), v.ID)
			}
			if calls.Load() != int64(v.Calls) {
				t.Fatal("dataset request count differs")
			}
			if !v.Valid {
				assertError(t, err, v.Code, v.Status, v.Message)
				return
			}
			if err != nil {
				t.Fatal("valid dataset contract refused")
			}
			if v.RequestURL == nil || <-observed != *v.RequestURL {
				t.Fatal("dataset query/flags differ")
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			json.Unmarshal(encoded, &a)
			json.Unmarshal(v.Value, &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatal("dataset normalized response differs")
			}
		})
	}
}

func assertError(t *testing.T, err error, code string, status int, message string) {
	t.Helper()
	var got *Error
	if !errors.As(err, &got) || got.Code != code || got.Status != status || (message != "" && err.Error() != message) {
		t.Fatalf("legacy Apify error mapping differs (%s/%d)", code, status)
	}
}
