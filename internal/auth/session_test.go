package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func createManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(strings.Repeat("x", 64), map[string]string{"eugene": "password!", "nastya": "another"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestAuthenticationAndLegacySessions(t *testing.T) {
	m := createManager(t)
	u, ok := m.Authenticate(" EUGENE ", "password!")
	if !ok || u.Name != "Eugene" || !u.IsAdmin {
		t.Fatalf("failed login: %+v", u)
	}
	for _, pair := range [][2]string{{"eugene", "bad"}, {"demo", "password!"}, {"unknown", "password!"}} {
		if _, ok := m.Authenticate(pair[0], pair[1]); ok {
			t.Fatalf("unauthorized login %q", pair[0])
		}
	}
	now := time.Date(2026, 10, 23, 17, 0, 0, 0, time.UTC)
	token, err := m.Issue(u, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, "v2.") || !strings.Contains(token, "."+base64.RawURLEncoding.EncodeToString([]byte("Eugene"))+".") {
		t.Fatalf("unexpected token format: %q", token)
	}
	if got, ok := m.Verify(token, now); !ok || got != u {
		t.Fatalf("cannot verify token: %+v", got)
	}
	if _, ok := m.Verify(token, now.Add(SessionTTL)); ok {
		t.Fatal("expired token accepted")
	}
	if _, ok := m.Verify(token[:len(token)-1]+"x", now); ok {
		t.Fatal("tampered signature accepted")
	}
	if _, ok := m.Verify(strings.Repeat("X", 5000), now); ok {
		t.Fatal("giant token accepted")
	}
	if _, ok := m.Verify("v2.nonsense.ZW1wdHk.AAAA", now); ok {
		t.Fatal("bad token accepted")
	}
}
func TestAuthConfiguration(t *testing.T) {
	if _, err := NewManager("small", map[string]string{"eugene": "p"}); err == nil {
		t.Fatal("weak key accepted")
	}
	if _, err := NewManager(strings.Repeat("x", 64), nil); err == nil {
		t.Fatal("missing admin password accepted")
	}
}
