// Package contacts implements the source hiring-contact contract.
package contacts

import (
	"bytes"
	"encoding/json"
	"errors"
	whatwg "github.com/nlnwa/whatwg-url/url"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
)

var ErrInvalidInput = errors.New("invalid contact input")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrInvalidInput }

// Input retains exact field spelling and null/missing position fallback. Other
// fields use the source replacement semantics, even on PUT.
type Input map[string]json.RawMessage

type NormalizedInput struct {
	CompanyName    string `json:"companyName"`
	Position       int    `json:"position"`
	FullName       string `json:"fullName"`
	LinkedinURL    string `json:"linkedinUrl"`
	FacebookURL    string `json:"facebookUrl"`
	SetkaURL       string `json:"setkaUrl"`
	Telegram       string `json:"telegram"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Specialization string `json:"specialization"`
}

const contactWhitespace = "\u0009\u000A\u000B\u000C\u000D\u0020\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFF"

func Trim(s string) string { return strings.Trim(s, contactWhitespace) }
func units(s string) int   { return len(utf16.Encode([]rune(s))) }
func decode(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	e := d.Decode(&v)
	return v, e
}
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case json.Number:
		f, _ := x.Float64()
		return f != 0 && !math.IsNaN(f)
	}
	return true
}
func jsNumberText(f float64) string {
	if f == 0 {
		return "0"
	}
	format := byte('f')
	if math.Abs(f) >= 1e21 || math.Abs(f) < 1e-6 {
		format = 'e'
	}
	s := strconv.FormatFloat(f, format, -1, 64)
	s = strings.ReplaceAll(s, "e-0", "e-")
	s = strings.ReplaceAll(s, "e+0", "e+")
	return s
}
func jsString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case string:
		return x, nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case json.Number:
		f, e := x.Float64()
		if e != nil && !math.IsInf(f, 0) {
			return "", e
		}
		if math.IsInf(f, 1) {
			return "Infinity", nil
		}
		if math.IsInf(f, -1) {
			return "-Infinity", nil
		}
		return jsNumberText(f), nil
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			if item == nil {
				continue
			}
			s, e := jsString(item)
			if e != nil {
				return "", e
			}
			parts[i] = s
		}
		return strings.Join(parts, ","), nil
	case map[string]any:
		if _, own := x["toString"]; own {
			return "", errors.New("Cannot convert object to primitive value")
		}
		return "[object Object]", nil
	}
	return "", errors.New("Cannot convert object to primitive value")
}
func inputString(raw json.RawMessage) (string, error) {
	v, e := decode(raw)
	if e != nil {
		return "", e
	}
	if !truthy(v) {
		return "", nil
	}
	s, e := jsString(v)
	return Trim(s), e
}
func inputNumber(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 {
		return math.NaN(), nil
	}
	v, e := decode(raw)
	if e != nil {
		return 0, e
	}
	switch x := v.(type) {
	case nil:
		return 0, nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case json.Number:
		f, _ := x.Float64()
		return f, nil
	}
	s, e := jsString(v)
	if e != nil {
		return 0, e
	}
	s = Trim(s)
	if s == "" {
		return 0, nil
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'b', 'B':
			base = 2
		case 'o', 'O':
			base = 8
		}
		if base != 0 {
			n, e := strconv.ParseUint(s[2:], base, 64)
			if e != nil {
				return math.NaN(), nil
			}
			return float64(n), nil
		}
	}
	if s == "NaN" || s == "Inf" || s == "+Inf" || s == "-Inf" || strings.Contains(s, "_") {
		return math.NaN(), nil
	}
	f, e := strconv.ParseFloat(s, 64)
	if e != nil {
		return math.NaN(), nil
	}
	return f, nil
}

// Parse only for validation. Return the exact trimmed source string, never a
// rewritten URL. WHATWG is required for backslashes, IDNA, IPv4 and ports;
// net/url alone has a materially different acceptance grammar.
func validLink(s string) bool {
	u, e := whatwg.Parse(s)
	return e == nil && (u.Scheme() == "http" || u.Scheme() == "https")
}
func NormalizeInput(raw Input, company string, currentPosition *int) (NormalizedInput, error) {
	fail := func(message string) (NormalizedInput, error) {
		return NormalizedInput{}, &ValidationError{Message: message}
	}
	positionRaw := raw["position"]
	if currentPosition != nil && (len(positionRaw) == 0 || bytes.Equal(bytes.TrimSpace(positionRaw), []byte("null"))) {
		positionRaw, _ = json.Marshal(*currentPosition)
	}
	position, e := inputNumber(positionRaw)
	if e != nil {
		return NormalizedInput{}, e
	}
	out := NormalizedInput{}
	for _, f := range []struct {
		key string
		dst *string
	}{{"fullName", &out.FullName}, {"linkedinUrl", &out.LinkedinURL}, {"facebookUrl", &out.FacebookURL}, {"setkaUrl", &out.SetkaURL}, {"telegram", &out.Telegram}, {"phone", &out.Phone}, {"email", &out.Email}, {"specialization", &out.Specialization}} {
		*f.dst, e = inputString(raw[f.key])
		if e != nil {
			return NormalizedInput{}, e
		}
	}
	if math.IsNaN(position) || position < 1 || position > 3 || math.Trunc(position) != position {
		return fail("позиция контакта должна быть целым числом от 1 до 3")
	}
	out.Position = int(position)
	if out.FullName == "" {
		return fail("ФИО контакта обязательно")
	}
	if units(out.FullName) > 256 {
		return fail("ФИО контакта не должно превышать 256 символов")
	}
	for _, f := range []struct{ value, label string }{{out.LinkedinURL, "LinkedIn"}, {out.FacebookURL, "Facebook"}, {out.SetkaURL, "Сетка"}} {
		if f.value != "" && (units(f.value) > 2048 || !validLink(f.value)) {
			return fail(f.label + ": укажите корректную ссылку")
		}
	}
	if units(out.Telegram) > 128 {
		return fail("Telegram не должен превышать 128 символов")
	}
	if units(out.Phone) > 64 {
		return fail("Номер телефона не должен превышать 64 символа")
	}
	if out.Email != "" {
		at := strings.IndexByte(out.Email, '@')
		valid := at > 0 && at == strings.LastIndexByte(out.Email, '@')
		if valid {
			domain := out.Email[at+1:]
			valid = false
			for i := 1; i < len(domain)-1; i++ {
				if domain[i] == '.' {
					valid = true
					break
				}
			}
		}
		for _, r := range out.Email {
			if strings.ContainsRune(contactWhitespace, r) {
				valid = false
			}
		}
		if units(out.Email) > 254 || !valid {
			return fail("Email: укажите корректный адрес")
		}
	}
	if units(out.Specialization) > 512 {
		return fail("Специализация контакта не должна превышать 512 символов")
	}
	if company != "" {
		out.CompanyName = Trim(company)
	} else {
		out.CompanyName, e = inputString(raw["companyName"])
		if e != nil {
			return NormalizedInput{}, e
		}
	}
	return out, nil
}

// OwnerName applies the source POST companyName coercion before catalog lookup.
func OwnerName(in Input) (string, error) { return inputString(in["companyName"]) }
