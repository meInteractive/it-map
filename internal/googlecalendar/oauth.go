// Package googlecalendar реализует первое подключение Google Calendar без
// переноса исторических OAuth-токенов или сетевых вызовов при запуске.
package googlecalendar

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"itmap/internal/legacycrypto"
)

var ErrConfiguration = errors.New("google calendar is not configured")
var ErrState = errors.New("invalid or expired google oauth state")
var ErrProvider = errors.New("google oauth provider unavailable")
var ErrStorage = errors.New("google calendar storage unavailable")
var ErrRefreshToken = errors.New("google refresh token was not returned")

const StateCookie = "google_calendar_oauth_state"
const stateTTL = 10 * time.Minute
const maximumTokenResponse = 16 << 10
const googleAuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
const googleTokenURL = "https://oauth2.googleapis.com/token"
const googleEmailURL = "https://www.googleapis.com/oauth2/v2/userinfo"

// Service не выполняет сетевых вызовов до явного OAuth callback.
type Service struct {
	DB           *sql.DB
	ClientID     string
	ClientSecret string
	Secret       string
	Enabled      bool
	Client       *http.Client
}

func (s *Service) Configured() bool {
	return s != nil && s.Enabled && s.DB != nil && s.ClientID != "" && s.ClientSecret != "" && len(s.Secret) >= 32
}

func (s *Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (s *Service) State(user string, now time.Time) (string, error) {
	if !s.Configured() || user == "" {
		return "", ErrConfiguration
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", ErrConfiguration
	}
	// Версия, срок, криптографический nonce, canonical owner и HMAC.
	payload := "v1." + strconv.FormatInt(now.Add(stateTTL).UnixMilli(), 10) + "." +
		base64.RawURLEncoding.EncodeToString(nonce) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(user))
	h := hmac.New(sha256.New, []byte(s.Secret))
	_, _ = h.Write([]byte("google-calendar-oauth-state:" + payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}

func (s *Service) ValidateState(cookie, query string, now time.Time) (string, error) {
	if !s.Configured() || len(query) > 2048 || len(cookie) > 2048 || query == "" ||
		!hmac.Equal([]byte(cookie), []byte(query)) {
		return "", ErrState
	}
	parts := strings.Split(query, ".")
	if len(parts) != 5 || parts[0] != "v1" {
		return "", ErrState
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || exp <= now.UnixMilli() || exp > now.Add(stateTTL).UnixMilli() {
		return "", ErrState
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(nonce) != 24 {
		return "", ErrState
	}
	h := hmac.New(sha256.New, []byte(s.Secret))
	_, _ = h.Write([]byte("google-calendar-oauth-state:" + strings.Join(parts[:4], ".")))
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[4])
	if err != nil || !hmac.Equal(signature, h.Sum(nil)) {
		return "", ErrState
	}
	user, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || (string(user) != "Eugene" && string(user) != "Nastya" && string(user) != "Demo") {
		return "", ErrState
	}
	return string(user), nil
}

func (s *Service) AuthorizationURL(callback, state string) (string, error) {
	if !s.Configured() {
		return "", ErrConfiguration
	}
	u, err := url.Parse(googleAuthURL)
	if err != nil {
		return "", ErrConfiguration
	}
	q := u.Query()
	q.Set("client_id", s.ClientID)
	q.Set("redirect_uri", callback)
	q.Set("response_type", "code")
	q.Set("scope", "https://www.googleapis.com/auth/calendar.events https://www.googleapis.com/auth/userinfo.email")
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (s *Service) exchange(ctx context.Context, code, callback string) (string, string, error) {
	form := url.Values{"code": {code}, "client_id": {s.ClientID}, "client_secret": {s.ClientSecret}, "redirect_uri": {callback}, "grant_type": {"authorization_code"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", ErrProvider
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client().Do(request)
	if err != nil {
		return "", "", ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", ErrProvider
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maximumTokenResponse+1))
	if err != nil || len(raw) > maximumTokenResponse {
		return "", "", ErrProvider
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(raw, &result) != nil || result.AccessToken == "" {
		return "", "", ErrProvider
	}
	if result.RefreshToken == "" {
		return "", "", ErrRefreshToken
	}
	return result.AccessToken, result.RefreshToken, nil
}

func (s *Service) email(ctx context.Context, token string) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, googleEmailURL, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client().Do(request)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil || len(body) >= 4096 {
		return ""
	}
	var result struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(body, &result) != nil || !strings.Contains(result.Email, "@") || len(result.Email) > 255 {
		return ""
	}
	return result.Email
}

func (s *Service) Connect(ctx context.Context, user, code, callback string) error {
	if !s.Configured() {
		return ErrConfiguration
	}
	if code == "" || len(code) > 4096 || len(callback) > 2048 {
		return ErrState
	}
	access, refresh, err := s.exchange(ctx, code, callback)
	if err != nil {
		return err
	}
	codec, err := legacycrypto.New(s.Secret, legacycrypto.Calendar)
	if err != nil {
		return ErrConfiguration
	}
	encrypted, err := codec.Encrypt(refresh)
	if err != nil {
		return ErrStorage
	}
	// Дополнительная информация об адресе не влияет на успешность OAuth.
	email := s.email(ctx, access)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO google_calendar_user_connection
		(user_name,account_email,refresh_token_encrypted,calendar_id,connected_at,updated_at)
		VALUES (?, ?, ?, 'primary', UTC_TIMESTAMP(), UTC_TIMESTAMP())
		ON DUPLICATE KEY UPDATE account_email=VALUES(account_email),
		refresh_token_encrypted=VALUES(refresh_token_encrypted),updated_at=UTC_TIMESTAMP()`, user, email, encrypted)
	if err != nil {
		return ErrStorage
	}
	return nil
}

type Status struct {
	Configured   bool   `json:"configured"`
	Connected    bool   `json:"connected"`
	AccountEmail string `json:"accountEmail"`
	SyncedTasks  int64  `json:"syncedTasks"`
	SyncErrors   int64  `json:"syncErrors"`
}

func (s *Service) ReadStatus(ctx context.Context, user string) (Status, error) {
	result := Status{Configured: s.Configured()}
	if s == nil || s.DB == nil {
		return result, ErrStorage
	}
	var email string
	err := s.DB.QueryRowContext(ctx, "SELECT account_email FROM google_calendar_user_connection WHERE user_name=?", user).Scan(&email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, ErrStorage
	}
	if err == nil {
		result.Connected = true
		result.AccountEmail = email
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT
		COUNT(CASE WHEN google_event_id IS NOT NULL AND google_event_id<>'' THEN 1 END),
		COUNT(CASE WHEN google_sync_status='error' THEN 1 END)
		FROM company_tasks WHERE user_name=?`, user).Scan(&result.SyncedTasks, &result.SyncErrors); err != nil {
		return Status{}, ErrStorage
	}
	return result, nil
}

// Disconnect удаляет только OAuth-связь. Старые ссылки задач на Google event
// не трогаются; их дальнейшая политика относится к следующему этапу sync.
func (s *Service) Disconnect(ctx context.Context, user string) error {
	if s == nil || s.DB == nil {
		return ErrStorage
	}
	_, err := s.DB.ExecContext(ctx, "DELETE FROM google_calendar_user_connection WHERE user_name=?", user)
	if err != nil {
		return ErrStorage
	}
	return nil
}

// Используется тестами, чтобы не пропускать ошибку формата URL callback.
func ValidateCallbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")
}

