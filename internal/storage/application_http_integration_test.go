//go:build mariadb && receiptwrites

package storage

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
)

type receiptHTTP struct {
	f                       *receiptFixture
	server                  *httptest.Server
	demo, nastya, anonymous *http.Client
}

func openReceiptHTTP(f *receiptFixture, creator httpapi.ApplicationCreator) *receiptHTTP {
	f.t.Helper()
	manager, err := auth.NewManager(string(f.repo.CreationSecret), map[string]string{
		"eugene": "CI_SYNTHETIC_HTTP_PASSWORD", "demo": "CI_SYNTHETIC_HTTP_PASSWORD", "nastya": "CI_SYNTHETIC_HTTP_PASSWORD"})
	if err != nil {
		f.t.Fatal("cannot initialize synthetic HTTP auth")
	}
	handler := httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{
		DB: f.db, Auth: manager, Applications: f.repo, ApplicationCompanies: f.repo,
		ApplicationCreates: creator, ApplicationWrites: f.repo, VacancyApplies: f.repo})
	server := httptest.NewTLSServer(handler)
	f.t.Cleanup(server.Close)
	h := &receiptHTTP{f: f, server: server}
	h.demo = h.login("demo")
	h.nastya = h.login("nastya")
	h.anonymous = h.client()
	return h
}
func (h *receiptHTTP) client() *http.Client {
	h.f.t.Helper()
	client := *h.server.Client() // Separate session state; share the test certificate transport.
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.f.t.Fatal("cannot initialize synthetic session jar")
	}
	client.Jar = jar
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
func (h *receiptHTTP) login(login string) *http.Client {
	h.f.t.Helper()
	client := h.client()
	form := url.Values{"login": {login}, "password": {"CI_SYNTHETIC_HTTP_PASSWORD"}}
	req, err := http.NewRequestWithContext(h.f.ctx, "POST", h.server.URL+"/auth", strings.NewReader(form.Encode()))
	if err != nil {
		h.f.t.Fatal("cannot construct synthetic login request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", h.server.URL)
	resp, err := client.Do(req)
	if err != nil {
		h.f.t.Fatal("synthetic HTTPS login failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 303 || resp.Header.Get("Location") != "/" {
		h.f.t.Fatal("synthetic login must redirect to the application")
	}
	valid := false
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName && c.Value != "" && c.Secure && c.HttpOnly && c.SameSite == http.SameSiteStrictMode && c.MaxAge > 0 {
			valid = true
		}
	}
	if !valid {
		h.f.t.Fatal("login did not issue the protected synthetic session cookie")
	}
	return client
}
func (h *receiptHTTP) call(client *http.Client, method, path string, payload any, want int, headers ...http.Header) []byte {
	h.f.t.Helper()
	var body []byte
	var err error
	if raw, ok := payload.(string); ok {
		body = []byte(raw)
	} else if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			h.f.t.Fatal("cannot encode synthetic HTTP payload")
		}
	}
	req, err := http.NewRequestWithContext(h.f.ctx, method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		h.f.t.Fatal("cannot construct synthetic API request")
	}
	if method != "GET" {
		req.Header.Set("Origin", h.server.URL)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, header := range headers {
		for name, values := range header {
			req.Header[name] = values
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		h.f.t.Fatal("synthetic HTTPS API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		h.f.t.Fatalf("%s %s: status %d, expected %d", method, path, resp.StatusCode, want)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") ||
		!strings.Contains(resp.Header.Get("Cache-Control"), "private") || !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		h.f.t.Fatal("personal API response must be JSON/private/no-store")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		h.f.t.Fatal("cannot read bounded synthetic API response")
	}
	return data
}
func receiptHTTPJSON[T any](t *testing.T, data []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal("unexpected synthetic API JSON shape")
	}
	return value
}
func (h *receiptHTTP) error(data []byte, code string) {
	h.f.t.Helper()
	value := receiptHTTPJSON[struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}](h.f.t, data)
	if value.Error == "" || value.Code != code {
		h.f.t.Fatal("unexpected API error contract")
	}
}
func (h *receiptHTTP) creationContext(client *http.Client) string {
	h.f.t.Helper()
	data := h.call(client, "GET", "/api/applications/create-context", nil, 200)
	fields := receiptHTTPJSON[map[string]string](h.f.t, data)
	if len(fields) != 1 || !strings.HasPrefix(fields["creationContext"], "ac1.") || len(fields["creationContext"]) != 47 {
		h.f.t.Fatal("create-context exposed unexpected fields or an invalid token")
	}
	return fields["creationContext"]
}

func TestMariaDBApplicationHTTPDisposable(t *testing.T) {
	t.Run("authenticated roundtrip and errors", func(t *testing.T) {
		f := openReceiptFixture(t) // Guards run before the HTTPS listener or any write.
		h := openReceiptHTTP(f, f.repo)
		h.error(h.call(h.anonymous, "GET", "/api/applications/create-context", nil, 401), "")
		h.error(h.call(h.anonymous, "POST", "/api/applications/create", f.input, 401), "")
		input := f.input
		input.CreationContext = h.creationContext(h.demo)
		if h.creationContext(h.nastya) == input.CreationContext {
			t.Fatal("creation context is not scoped to the authenticated user")
		}
		h.error(h.call(h.nastya, "POST", "/api/applications/create", input, 409), "creation_context_expired")
		h.error(h.call(h.demo, "POST", "/api/applications/create", input, 403, http.Header{"Origin": {"https://cross-site.invalid"}}), "")
		h.error(h.call(h.demo, "POST", "/api/applications/create", input, 403, http.Header{"Sec-Fetch-Site": {"cross-site"}}), "")
		h.error(h.call(h.demo, "POST", "/api/applications/create", input, 400, http.Header{"Content-Type": {"text/plain"}}), "")
		h.error(h.call(h.demo, "POST", "/api/applications/create", "{} {}", 400), "")
		missing := input
		missing.IdempotencyKey = ""
		h.error(h.call(h.demo, "POST", "/api/applications/create", missing, 428), "creation_context_required")
		unknown := input
		unknown.CompanyName = "CI Unknown Synthetic Employer"
		h.error(h.call(h.demo, "POST", "/api/applications/create", unknown, 404), "")
		invalid := input
		missingID := int64(999999)
		invalid.VacancyID = &missingID
		h.error(h.call(h.demo, "POST", "/api/applications/create", invalid, 400), "")
		f.assertCounts(0, 0, 0)

		vacancy := int64(1)
		input.VacancyID = &vacancy
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal("cannot encode synthetic intent")
		}
		spoof := receiptHTTPJSON[map[string]any](t, encoded)
		spoof["userName"] = "Eugene" // User ownership must come only from the cookie.
		created := receiptHTTPJSON[applications.CreationResult](t, h.call(h.demo, "POST", "/api/applications/create", spoof, 201))
		if created.CreatedApplicationID <= 1 || created.Replayed || created.CompanyName != receiptCompany || created.Limit != 10 ||
			created.Favorite || created.Hidden || len(created.Records) != 1 || len(created.Vacancies) != 1 || created.Records[0].ID != created.CreatedApplicationID {
			t.Fatal("create response is not the complete personal company result")
		}
		f.assertCounts(1, 1, 1)
		path := fmt.Sprintf("/api/applications/%d", created.CreatedApplicationID)
		detail := receiptHTTPJSON[struct {
			Record applications.Record `json:"record"`
		}](t, h.call(h.demo, "GET", path, nil, 200)).Record
		if len(detail.Revision) != 64 || len(detail.History) != 1 || detail.Comment != input.Comment {
			t.Fatal("HTTP detail lost revision/fields/history")
		}
		h.error(h.call(h.anonymous, "GET", path, nil, 401), "")
		h.error(h.call(h.nastya, "GET", path, nil, 404), "")
		h.error(h.call(h.nastya, "DELETE", path, nil, 404), "")
		foreign := receiptHTTPJSON[applications.CompanyResponse](t, h.call(h.nastya, "GET", "/api/applications?company="+url.QueryEscape(receiptCompany), nil, 200))
		if len(foreign.Records) != 0 {
			t.Fatal("another user's application leaked through the company response")
		}
		replay := receiptHTTPJSON[applications.CreationResult](t, h.call(h.demo, "POST", "/api/applications", input, 200))
		if !replay.Replayed || replay.CreatedApplicationID != created.CreatedApplicationID {
			t.Fatal("POST alias did not replay the durable identity")
		}
		changed := input
		changed.Comment = "CI synthetic conflicting HTTP intent"
		h.error(h.call(h.demo, "POST", "/api/applications/create", changed, 409), "intent_conflict")
		duplicate := input
		duplicate.IdempotencyKey = receiptKey(2)
		h.error(h.call(h.demo, "POST", "/api/applications/create", duplicate, 400), "")
		f.assertCounts(1, 1, 1)

		update := updateFromRecord(detail)
		update.Status, update.Comment = "Скрининг", "CI synthetic HTTP edited application"
		noRevision := update
		noRevision.Revision = ""
		h.error(h.call(h.demo, "PUT", path, noRevision, 428), "")
		updated := receiptHTTPJSON[applications.CompanyResponse](t, h.call(h.demo, "PUT", path, update, 200))
		if len(updated.Records) != 1 || updated.Records[0].Revision == detail.Revision || updated.Records[0].Comment != update.Comment || len(updated.Records[0].History) != 2 {
			t.Fatal("HTTP update lost fields/revision or atomic status history")
		}
		h.error(h.call(h.demo, "PUT", path, update, 409), "")
		replay = receiptHTTPJSON[applications.CreationResult](t, h.call(h.demo, "POST", "/api/applications/create", input, 200))
		if !replay.Replayed || replay.CreatedApplicationID != created.CreatedApplicationID || len(replay.Records) != 1 || replay.Records[0].Comment != update.Comment || len(replay.Records[0].History) != 2 {
			t.Fatal("create replay overwrote a later HTTP edit")
		}
		f.assertCounts(1, 1, 2)
		h.error(h.call(h.demo, "DELETE", path, nil, 403, http.Header{"Origin": {"https://cross-site.invalid"}}), "")
		deleted := receiptHTTPJSON[applications.CompanyResponse](t, h.call(h.demo, "DELETE", path, nil, 200))
		if deleted.CompanyName != receiptCompany || len(deleted.Records) != 0 || len(deleted.Vacancies) != 1 {
			t.Fatal("delete response lost the complete company result")
		}
		f.assertCounts(0, 1, 0)
		h.error(h.call(h.demo, "POST", "/api/applications", input, 410), "application_deleted")
		h.error(h.call(h.demo, "DELETE", path, nil, 404), "")
		f.assertCounts(0, 1, 0)
	})
	t.Run("HTTP 503 after commit is replayable", func(t *testing.T) {
		f := openReceiptFixture(t)
		cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if err != nil {
			t.Fatal("cannot configure guarded HTTP fault connection")
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatal("cannot initialize guarded HTTP fault connector")
		}
		fault := &receiptFault{stage: "response"}
		db := sql.OpenDB(receiptFaultConnector{Connector: connector, fault: fault})
		t.Cleanup(func() { db.Close() })
		repo := applications.Repository{DB: db, CreationSecret: f.repo.CreationSecret}
		h := openReceiptHTTP(f, repo)
		input := f.input
		input.CreationContext = h.creationContext(h.demo)
		data := h.call(h.demo, "POST", "/api/applications/create", input, 503)
		h.error(data, "")
		if strings.Contains(string(data), errReceiptInterruption.Error()) {
			t.Fatal("HTTP exposed an internal SQL error")
		}
		fault.mu.Lock()
		committed, fired := fault.committed, fault.fired
		fault.mu.Unlock()
		if !committed || !fired {
			t.Fatal("HTTP error did not follow the armed real commit")
		}
		f.assertCounts(1, 1, 1)
		var savedID int64
		if err := f.db.QueryRowContext(f.ctx, "SELECT created_application_id FROM application_creation_receipts WHERE user_name=? AND idempotency_key=?", receiptUser, input.IdempotencyKey).Scan(&savedID); err != nil {
			t.Fatal("committed HTTP receipt is unavailable")
		}
		replay := receiptHTTPJSON[applications.CreationResult](t, h.call(h.demo, "POST", "/api/applications", input, 200))
		if !replay.Replayed || replay.CreatedApplicationID != savedID || len(replay.Records) != 1 || len(replay.Records[0].History) != 1 {
			t.Fatal("retry after HTTP 503 duplicated or lost the committed result")
		}
		f.assertCounts(1, 1, 1)
	})
}
