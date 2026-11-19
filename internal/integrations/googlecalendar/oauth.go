package googlecalendar

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	StateTTL      = 10 * time.Minute
	CalendarScope = "https://www.googleapis.com/auth/calendar.events"
	EmailScope    = "https://www.googleapis.com/auth/userinfo.email"
	authEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
)

var (
	ErrConfiguration = errors.New("google calendar oauth not configured")
	ErrState         = errors.New("google calendar oauth state rejected")
	ErrProvider      = errors.New("google calendar provider unavailable")
	ErrRefreshToken  = errors.New("google calendar refresh token not issued")
)

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	StateSecret  string
}

type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    int
}

// Конфигурация привязана к фиксированному HTTPS redirect URI, а не к Host
// или X-Forwarded-Host входящего запроса.
func (c Config) Valid() bool {
	u, err := url.Parse(c.RedirectURI)
	return c.ClientID != "" && c.ClientSecret != "" && len(c.StateSecret) >= 32 &&
		err == nil && u.IsAbs() && u.Scheme == "https" && u.Host != "" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/api/google-calendar/callback"
}

func (c Config) mac(value string) []byte {
	k := hmac.New(sha256.New, []byte(c.StateSecret))
	_, _ = k.Write([]byte("itmap-google-calendar-v1"))
	h := hmac.New(sha256.New, k.Sum(nil))
	_, _ = h.Write([]byte(value))
	return h.Sum(nil)
}

func validUser(s string) bool {
	if len(s) == 0 || len(s) > 255 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, ch := range s {
		if ch < 32 || ch == 127 {
			return false
		}
	}
	return true
}

// State связывает короткий срок, случайную nonce и авторизованного пользователя
// одной HMAC. Обязательная cookie связывает callback с исходным браузером.
func (c Config) NewState(user string, now time.Time) (string, error) {
	if !c.Valid() || !validUser(user) {
		return "", ErrConfiguration
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrConfiguration
	}
	enc := base64.RawURLEncoding
	body := strings.Join([]string{
		"v1", strconv.FormatInt(now.Add(StateTTL).Unix(), 10),
		enc.EncodeToString(nonce), enc.EncodeToString([]byte(user)),
	}, ".")
	return body + "." + enc.EncodeToString(c.mac(body)), nil
}

func (c Config) VerifyState(state, cookie string, now time.Time) (string, error) {
	if !c.Valid() || len(state) < 30 || len(state) > 2048 ||
		len(cookie) != len(state) || !hmac.Equal([]byte(state), []byte(cookie)) {
		return "", ErrState
	}
	parts := strings.Split(state, ".")
	if len(parts) != 5 || parts[0] != "v1" {
		return "", ErrState
	}
	enc := base64.RawURLEncoding.Strict()
	body := strings.Join(parts[:4], ".")
	mac, err := enc.DecodeString(parts[4])
	if err != nil || !hmac.Equal(mac, c.mac(body)) {
		return "", ErrState
	}
	expires, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expires <= now.Unix() || expires > now.Add(StateTTL).Unix() {
		return "", ErrState
	}
	nonce, err := enc.DecodeString(parts[2])
	if err != nil || len(nonce) != 24 {
		return "", ErrState
	}
	raw, err := enc.DecodeString(parts[3])
	if err != nil || !validUser(string(raw)) {
		return "", ErrState
	}
	return string(raw), nil
}

// CodeVerifier не раскрывается браузеру. PKCE привязан к конкретному state.
func (c Config) CodeVerifier(state string) string {
	return base64.RawURLEncoding.EncodeToString(c.mac("pkce:" + state))
}

func (c Config) AuthorizeURL(state string) (string, error) {
	if !c.Valid() || state == "" || len(state) > 2048 {
		return "", ErrConfiguration
	}
	digest := sha256.Sum256([]byte(c.CodeVerifier(state)))
	v := url.Values{
		"client_id":             {c.ClientID},
		"redirect_uri":          {c.RedirectURI},
		"response_type":         {"code"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"scope":                 {CalendarScope + " " + EmailScope},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(digest[:])},
		"code_challenge_method": {"S256"},
	}
	return authEndpoint + "?" + v.Encode(), nil
}

type TokenClient struct {
	HTTP *http.Client
}

func (t TokenClient) call(ctx context.Context, form url.Values) (Token, error) {
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, ErrProvider
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := copyClient.Do(req)
	if err != nil {
		return Token{}, ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Token{}, ErrProvider
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 16*1024))
	if d.Decode(&raw) != nil || raw.AccessToken == "" ||
		!strings.EqualFold(raw.TokenType, "bearer") || raw.ExpiresIn <= 0 {
		return Token{}, ErrProvider
	}
	return Token{raw.AccessToken, raw.RefreshToken, raw.TokenType, raw.ExpiresIn}, nil
}

func (t TokenClient) Exchange(ctx context.Context, c Config, code, state string) (Token, error) {
	if !c.Valid() || code == "" || len(code) > 4096 || state == "" {
		return Token{}, ErrConfiguration
	}
	token, err := t.call(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"redirect_uri":  {c.RedirectURI},
		"code_verifier": {c.CodeVerifier(state)},
	})
	if err != nil {
		return Token{}, err
	}
	if token.RefreshToken == "" {
		return Token{}, ErrRefreshToken
	}
	return token, nil
}

func (t TokenClient) Refresh(ctx context.Context, c Config, refreshToken string) (Token, error) {
	if !c.Valid() || refreshToken == "" || len(refreshToken) > 8192 {
		return Token{}, ErrConfiguration
	}
	return t.call(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
	})
}
