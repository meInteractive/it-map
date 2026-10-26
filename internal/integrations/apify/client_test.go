package apify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestApifyLostStartDoesNotRetry(t *testing.T) {
	client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v2/actors/"+Actor+"/runs" {
			t.Error("unexpected start endpoint")
		}
		query := r.URL.Query()
		if query.Get("maxTotalChargeUsd") != "0.25" || query.Get("timeout") != "180" || query.Get("restartOnError") != "false" || query.Get("waitForFinish") != "0" {
			t.Error("unsafe actor start flags")
		}
		var body StartInput
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.MaxPages != 1 || body.ResultsWanted != 100 || !body.ProxyConfiguration.UseApifyProxy {
			t.Error("invalid start input")
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	_, err := client.Start(context.Background(), "https://hh.ru/employer/1", HHOptions{})
	if !IsStartUncertain(err) {
		t.Fatal("lost POST acknowledgement was not uncertain")
	}
	assertError(t, err, "apify_network", 503, "")
	if calls.Load() != 1 {
		t.Fatal("lost actor start retried")
	}
	_, err = client.Start(context.Background(), "https://ci.invalid/", HHOptions{})
	if IsStartUncertain(err) || calls.Load() != 1 {
		t.Fatal("invalid HH scope sent request or became an admitted start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Start(ctx, "https://hh.ru/employer/1", HHOptions{})
	if !errors.Is(err, context.Canceled) || IsStartUncertain(err) || calls.Load() != 1 {
		t.Fatal("pre-cancelled start sent a request")
	}
}
func TestApifyMalformedStartIsUncertain(t *testing.T) {
	client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"id":"Run123","status":"RUNNING"}}`)
	})
	_, err := client.Start(context.Background(), "https://hh.ru/vacancy/1", HHOptions{Detail: true})
	if !IsStartUncertain(err) || calls.Load() != 1 {
		t.Fatal("malformed POST reply lost uncertainty/no-retry")
	}
	assertError(t, err, "apify_data", 502, "")
}
func TestApifyCancellationAndTimeout(t *testing.T) {
	t.Run("caller cancel after request", func(t *testing.T) {
		seen := make(chan struct{})
		client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) { close(seen); <-r.Context().Done() })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := client.Run(ctx, "Run123"); done <- err }()
		select {
		case <-seen:
		case <-time.After(3 * time.Second):
			t.Fatal("mock request did not start")
		}
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
				t.Fatal("caller cancellation changed or retried")
			}
		case <-time.After(time.Second):
			t.Fatal("cancel did not stop request")
		}
	})
	t.Run("header timeout", func(t *testing.T) {
		client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		client.timeout = 50 * time.Millisecond
		_, err := client.Run(context.Background(), "Run123")
		assertError(t, err, "apify_timeout", 503, "")
		if calls.Load() != 1 {
			t.Fatal("timeout retried")
		}
	})
	t.Run("body timeout retains legacy network mapping", func(t *testing.T) {
		client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			io.WriteString(w, "{")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		client.timeout = 50 * time.Millisecond
		_, err := client.Run(context.Background(), "Run123")
		assertError(t, err, "apify_network", 503, "")
		if calls.Load() != 1 {
			t.Fatal("body timeout retried")
		}
	})
}
func TestApifyDatasetBoundsAndMetadata(t *testing.T) {
	for _, row := range []struct {
		name, body string
		valid      bool
	}{{"ten", `[{},{},{},{},{},{},{},{},{},{}]`, true}, {"eleven", `[{},{},{},{},{},{},{},{},{},{},{}]`, false}, {"null", `null`, false}, {"object", `{}`, false}} {
		t.Run(row.name, func(t *testing.T) {
			client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if q.Get("offset") != "10" || q.Get("limit") != "10" || q.Get("clean") != "false" || q.Get("skipEmpty") != "false" || q.Get("skipHidden") != "true" {
					t.Error("dataset request flags differ")
				}
				io.WriteString(w, row.body)
			})
			items, err := client.DatasetItems(context.Background(), "Data123", 10, false)
			if row.valid {
				if err != nil || len(items) != 10 {
					t.Fatal("ten-item page refused")
				}
			} else {
				assertError(t, err, "apify_data", 502, "")
			}
			_, err = client.DatasetItems(context.Background(), "bad-id", 0, true)
			assertError(t, err, "apify_data", 400, "")
			_, err = client.DatasetItems(context.Background(), "Data123", -1, true)
			assertError(t, err, "apify_data", 400, "")
			if calls.Load() != 1 {
				t.Fatal("invalid dataset input sent another request")
			}
		})
	}
	client, _ := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"id":"Data123","itemCount":12,"cleanItemCount":null,"modifiedAt":"2026-10-26T12:00:00.000Z"}}`)
	})
	info, err := client.DatasetInfo(context.Background(), "Data123")
	if err != nil || info.ItemCount != 12 || info.CleanItemCount != nil || info.ModifiedAt != "2026-10-26T12:00:00.000Z" {
		t.Fatal("dataset metadata changed")
	}
	for _, body := range []string{`{"data":{"id":"Other","itemCount":1}}`, `{"data":{"id":"Data123","itemCount":-1}}`, `{"data":{"id":"Data123","itemCount":"1"}}`, `{"data":{"id":"Data123"}}`} {
		client, _ := mockClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) })
		_, err := client.DatasetInfo(context.Background(), "Data123")
		assertError(t, err, "apify_data", 502, "")
	}
}
func TestApifyConcurrentReadsAndRedaction(t *testing.T) {
	client, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v2/actor-runs/")
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "defaultDatasetId": "Data123", "status": "SUCCEEDED", "usageTotalUsd": 0.02}})
	})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			run, err := client.Run(context.Background(), "Run123")
			if err != nil || run.Status != "SUCCEEDED" {
				t.Error("concurrent run read failed")
			}
		}()
	}
	workers.Wait()
	if calls.Load() != 8 {
		t.Fatal("concurrent reads changed request count")
	}
	if _, err := New(""); err == nil {
		t.Fatal("missing token accepted")
	}
	if _, err := New("CI\nSYNTHETIC"); err == nil {
		t.Fatal("header injection accepted")
	}
}
