// Package auth keeps the legacy three-account login and its v2 HMAC-SHA256
// cookie format compatible with GPT Sites. Credentials are never persisted.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

const CookieName = "it_map_session"
const SessionTTL = 30 * 24 * time.Hour

type User struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"isAdmin"`
}

type Manager struct {
	secret    []byte
	passwords map[string]string
}

var legacyUsers = map[string]User{
	"eugene": {Name: "Eugene", Role: "admin", IsAdmin: true},
	"nastya": {Name: "Nastya", Role: "user"},
	"demo":   {Name: "Demo", Role: "user"},
}

// NewManager deliberately rejects weak signing secrets and missing admin
// credentials, even on staging, so temporary setups cannot silently go live.
// passwords contains login keys (eugene, nastya, demo), NOT environment keys.
func NewManager(secret string, passwords map[string]string) (*Manager, error) {
	if len(secret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 bytes")
	}
	if passwords["eugene"] == "" {
		return nil, errors.New("SITE_PASSWORD is required")
	}
	safe := make(map[string]string, len(legacyUsers))
	for login := range legacyUsers {
		if p := passwords[login]; p != "" {
			safe[login] = p
		}
	}
	return &Manager{secret: []byte(secret), passwords: safe}, nil
}

func (m *Manager) sign(s string) []byte {
	h := hmac.New(sha256.New, m.secret)
	_, _ = h.Write([]byte(s))
	return h.Sum(nil)
}

func (m *Manager) Authenticate(login, password string) (User, bool) {
	if m == nil || password == "" {
		return User{}, false
	}
	normalized := strings.ToLower(strings.TrimSpace(login))
	user, ok := legacyUsers[normalized]
	expected := m.passwords[normalized]
	if !ok || expected == "" {
		return User{}, false
	}
	// Same HMAC comparison scheme as the original Sites Worker; avoids
	// comparing plaintext passwords with variable-time string equality.
	left, right := m.sign(password), m.sign(expected)
	return user, hmac.Equal(left, right)
}

func (m *Manager) Issue(u User, now time.Time) (string, error) {
	if m == nil {
		return "", errors.New("auth not configured")
	}
	canonical, ok := legacyUsers[strings.ToLower(u.Name)]
	if !ok || canonical.Name != u.Name {
		return "", errors.New("unknown account")
	}
	expiry := now.Add(SessionTTL).UnixMilli()
	encoded := base64.RawURLEncoding.EncodeToString([]byte(u.Name))
	payload := "v2." + strconv.FormatInt(expiry, 10) + "." + encoded
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.sign(payload)), nil
}

func (m *Manager) Verify(token string, now time.Time) (User, bool) {
	if m == nil || len(token) > 4096 || token == "" {
		return User{}, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v2" {
		return User{}, false
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiry <= now.UnixMilli() {
		return User{}, false
	}
	payload := strings.Join(parts[:3], ".")
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || !hmac.Equal(signature, m.sign(payload)) {
		return User{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return User{}, false
	}
	name := string(raw)
	user, ok := legacyUsers[strings.ToLower(name)]
	if !ok || user.Name != name {
		return User{}, false
	}
	return user, true
}
