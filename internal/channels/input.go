package channels

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
)

// Input preserves omitted/null fields for the legacy PUT fallback semantics.
// An explicit empty string is a value, including when it clears the check date.
type Input struct {
	Name                  *string `json:"name"`
	URL                   *string `json:"url"`
	Username              *string `json:"username"`
	Comment               *string `json:"comment"`
	LastSuccessfulCheckAt *string `json:"lastSuccessfulCheckAt"`
}

type NormalizedInput struct {
	Name                  string  `json:"name"`
	URL                   string  `json:"url"`
	NormalizedUsername    string  `json:"normalizedUsername"`
	Username              string  `json:"username"`
	LastSuccessfulCheckAt *string `json:"lastSuccessfulCheckAt"`
	Comment               string  `json:"comment"`
}

var ErrInvalidInput = errors.New("invalid channel input")

// ValidationError carries only a fixed legacy public message, never SQL/input.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }
func invalidInput(message string) (NormalizedInput, error) {
	return NormalizedInput{}, &ValidationError{Message: message}
}

var channelUsername = regexp.MustCompile(`^[a-z0-9_]{5,64}$`)
var httpPrefix = regexp.MustCompile(`(?i)^https?://`)

// jsTrim matches ECMAScript String.trim, including BOM but excluding NEL.
func jsTrim(value string) string {
	return strings.Trim(value, "\u0009\u000A\u000B\u000C\u000D\u0020\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFF")
}

// NormalizeUsername is for channel inputs, whose legacy 5..64 character rule
// permits initial digits. Vacancy message URLs have a separate contract.
// It never fetches or resolves the supplied address.
func NormalizeUsername(raw string) string {
	source := jsTrim(raw)
	if strings.HasPrefix(source, "@") {
		source = source[1:]
	} else {
		if !httpPrefix.MatchString(source) {
			source = "https://" + source
		}
		parsed, err := url.Parse(source)
		if err != nil {
			return ""
		}
		host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
		if host != "t.me" && host != "telegram.me" {
			return ""
		}
		parts := strings.FieldsFunc(parsed.EscapedPath(), func(c rune) bool { return c == '/' })
		if len(parts) > 0 && strings.EqualFold(parts[0], "s") {
			parts = parts[1:]
		}
		source = ""
		if len(parts) > 0 {
			source = parts[0]
		}
	}
	username := strings.ToLower(jsTrim(strings.TrimPrefix(source, "@")))
	if !channelUsername.MatchString(username) {
		return ""
	}
	return username
}

// NormalizeInput covers the typed UI payload and RFC3339 check dates. Wider
// JavaScript coercions and Date.parse grammars are not exposed as a new API.
// This pure function does not enable channel writes or mutate the old reader.
func NormalizeInput(input Input, current *Channel) (NormalizedInput, error) {
	pick := func(values ...*string) string {
		for _, v := range values {
			if v != nil {
				return *v
			}
		}
		return ""
	}
	var oldName, oldURL, oldUsername, oldComment, oldCheck *string
	if current != nil {
		oldName = &current.Name
		oldURL = &current.URL
		oldUsername = &current.Username
		oldComment = &current.Comment
		oldCheck = current.LastSuccessfulCheckAt
	}
	result := NormalizedInput{Name: jsTrim(pick(input.Name, oldName)), Comment: pick(input.Comment, oldComment)}
	result.NormalizedUsername = NormalizeUsername(pick(input.URL, input.Username, oldURL, oldUsername))
	if result.Name == "" {
		return invalidInput("Укажите название Telegram-канала.")
	}
	if len(utf16.Encode([]rune(result.Name))) > 160 {
		return invalidInput("Название канала не должно превышать 160 символов.")
	}
	if result.NormalizedUsername == "" {
		return invalidInput("Укажите корректную ссылку на публичный Telegram-канал или @username.")
	}
	if len(utf16.Encode([]rune(result.Comment))) > 512 {
		return invalidInput("Комментарий не должен превышать 512 символов.")
	}
	check := jsTrim(pick(input.LastSuccessfulCheckAt, oldCheck))
	if check != "" {
		date, err := time.Parse(time.RFC3339Nano, check)
		if err != nil || strings.Contains(check, ",") {
			return invalidInput("Укажите корректные дату и время последней проверки.")
		}
		if len(check) >= 6 {
			z := check[len(check)-6:]
			if (z[0] == '+' || z[0] == '-') && (z[1:3] > "23" || z[4:] > "59") {
				return invalidInput("Укажите корректные дату и время последней проверки.")
			}
		}
		formatted := date.UTC().Format("2006-01-02T15:04:05.000Z")
		result.LastSuccessfulCheckAt = &formatted
	}
	result.Username = "@" + result.NormalizedUsername
	result.URL = "https://t.me/" + result.NormalizedUsername
	return result, nil
}
