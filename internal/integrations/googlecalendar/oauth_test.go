package googlecalendar

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{ClientID: "CI_GOOGLE_CLIENT", ClientSecret: "CI_GOOGLE_SECRET", RedirectURI: "https://example.invalid/api/google-calendar/callback", StateSecret: strings.Repeat("K", 36)}
}

func TestOAuthStateAndPKCE(t *testing.T) {
	c := testConfig()
	now := time.Unix(1795000000, 0)
	state, err := c.NewState("CI_USER", now)
	if err != nil || strings.Contains(state, "CI_USER") {
		t.Fatal("state generation leaked user or failed", err)
	}
	second, err := c.NewState("CI_USER", now)
	if err != nil || state == second {
		t.Fatal("state nonce not random", err)
	}
	if user, err := c.VerifyState(state, state, now); err != nil || user != "CI_USER" {
		t.Fatal("same browser state rejected", err)
	}
	if _, err := c.VerifyState(state, second, now); err != ErrState {
		t.Fatal("different browser cookie accepted", err)
	}
	if _, err := c.VerifyState(state+"x", state+"x", now); err != ErrState {
		t.Fatal("tampered signature accepted", err)
	}
	if _, err := c.VerifyState(state, state, now.Add(StateTTL)); err != ErrState {
		t.Fatal("expired state accepted", err)
	}
	if _, err := c.VerifyState(state, state, now.Add(-time.Hour)); err != ErrState {
		t.Fatal("future expiration beyond TTL accepted", err)
	}
	authURL, err := c.AuthorizeURL(state)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil || u.Host != "accounts.google.com" || u.Query().Get("access_type") != "offline" ||
		u.Query().Get("scope") != CalendarScope+" "+EmailScope ||
		u.Query().Get("state") != state ||
		u.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("OAuth scopes / redirect / state not constrained", err)
	}
	hash := sha256.Sum256([]byte(c.CodeVerifier(state)))
	if u.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) ||
		strings.Contains(authURL, c.ClientSecret) {
		t.Fatal("PKCE or OAuth URL leaked secret")
	}
	for _, changed := range []Config{
		{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURI: "http://example.invalid/api/google-calendar/callback", StateSecret: c.StateSecret},
		{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURI: "https://example.invalid/api/google-calendar/callback?return=https://attacker.invalid", StateSecret: c.StateSecret},
		{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURI: "https://example.invalid/wrong", StateSecret: c.StateSecret},
		{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURI: c.RedirectURI, StateSecret: "weak"},
	} {
		if changed.Valid() {
			t.Fatal("unsafe OAuth config accepted")
		}
	}
}

type oauthTransport func(*http.Request) (*http.Response, error)
func (r oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }

func TestOAuthExchangeAndRefreshSynthetic(t *testing.T) {
	c := testConfig()
	now := time.Unix(1795000000, 0)
	state, _ := c.NewState("CI_USER", now)
	calls := 0
	client := TokenClient{HTTP: &http.Client{Transport: oauthTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != http.MethodPost || req.URL.String() != tokenEndpoint || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("unexpected token endpoint", req.URL)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		v, err := url.ParseQuery(string(body))
		if err != nil || v.Get("client_secret") != c.ClientSecret {
			t.Fatal("invalid OAuth form", err)
		}
		if calls == 1 && (v.Get("grant_type") != "authorization_code" || v.Get("code_verifier") != c.CodeVerifier(state)) {
			t.Fatal("authorization code/PKCE mismatch")
		}
		if calls == 2 && (v.Get("grant_type") != "refresh_token" || v.Get("refresh_token") != "CI_REFRESH") {
			t.Fatal("refresh token mismatch")
		}
		response := `{"access_token":"CI_ACCESS","refresh_token":"CI_REFRESH","token_type":"Bearer","expires_in":3599}`
		if calls == 2 {
			response = `{"access_token":"CI_ACCESS_2","token_type":"Bearer","expires_in":3599}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: http.Header{}}, nil
	})}}
	tok, err := client.Exchange(context.Background(), c, "CI_CODE", state)
	if err != nil || tok.RefreshToken != "CI_REFRESH" {
		t.Fatal("exchange failed", err)
	}
	next, err := client.Refresh(context.Background(), c, tok.RefreshToken)
	if err != nil || next.AccessToken != "CI_ACCESS_2" || next.RefreshToken != "" || calls != 2 {
		t.Fatal("refresh failed or requested twice", err, calls)
	}
}

func TestOAuthMissingRefreshAndProviderErrorsSynthetic(t *testing.T) {
	c := testConfig()
	client := TokenClient{HTTP: &http.Client{Transport: oauthTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"CI_ACCESS","token_type":"Bearer","expires_in":1}`)), Header: http.Header{}}, nil
	})}}
	_, err := client.Exchange(context.Background(), c, "CI_CODE", "CI_STATE")
	if err != ErrRefreshToken {
		t.Fatal("exchange accepted no refresh token", err)
	}
	client.HTTP.Transport = oauthTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error_description":"CI_PRIVATE_TOKEN"}`)), Header: http.Header{}}, nil
	})
	_, err = client.Exchange(context.Background(), c, "CI_CODE", "CI_STATE")
	if err != ErrProvider || strings.Contains(err.Error(), "CI_PRIVATE") {
		t.Fatal("provider body leaked", err)
	}
}
