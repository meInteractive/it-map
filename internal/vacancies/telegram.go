package vacancies

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var telegramMessageID = regexp.MustCompile(`^[0-9]+$`)
var telegramUsername = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{4,31}$`)

// CanonicalTelegramMessage mirrors the supported public t.me/s/name/id and
// t.me/name/id forms. Reject private invite links, unexpected hosts and
// links to another channel to protect vacancy provenance.
func CanonicalTelegramMessage(value, expected string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" {
		return "", errors.New("invalid Telegram URL")
	}
	host := strings.ToLower(strings.TrimPrefix(u.Hostname(), "www."))
	if host != "t.me" && host != "telegram.me" {
		return "", errors.New("invalid Telegram host")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && strings.EqualFold(parts[0], "s") {
		parts = parts[1:]
	}
	if len(parts) != 2 {
		return "", errors.New("Telegram message ID required")
	}
	user := strings.TrimPrefix(parts[0], "@")
	if !telegramUsername.MatchString(user) || !telegramMessageID.MatchString(parts[1]) {
		return "", errors.New("invalid Telegram message path")
	}
	user = strings.ToLower(user)
	expected = strings.ToLower(strings.TrimPrefix(expected, "@"))
	if expected != "" && user != expected {
		return "", errors.New("Telegram channel mismatch")
	}
	return "https://t.me/" + user + "/" + parts[1], nil
}
