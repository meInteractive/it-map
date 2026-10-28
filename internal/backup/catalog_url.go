package backup

import (
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// CatalogURLText decodes the percent-decoded bytes with the source WHATWG
// replacement rules. url.Values has already handled percent escapes and '+'.
func CatalogURLText(value string) string { return string(decodeUTF8([]byte(value))) }

// CatalogQueryValue follows URLSearchParams' form decoder: a malformed percent
// escape stays literal instead of making Go's url.ParseQuery discard the pair.
func CatalogQueryValue(raw, key string) string {
	decode := func(s string) string {
		hex := func(b byte) int {
			switch {
			case b >= '0' && b <= '9':
				return int(b - '0')
			case b >= 'a' && b <= 'f':
				return int(b-'a') + 10
			case b >= 'A' && b <= 'F':
				return int(b-'A') + 10
			}
			return -1
		}
		out := []byte{}
		for i := 0; i < len(s); i++ {
			if s[i] == '+' {
				out = append(out, ' ')
			} else if s[i] == '%' && i+2 < len(s) && hex(s[i+1]) >= 0 && hex(s[i+2]) >= 0 {
				out = append(out, byte(hex(s[i+1])*16+hex(s[i+2])))
				i += 2
			} else {
				out = append(out, s[i])
			}
		}
		return string(decodeUTF8(out))
	}
	for _, pair := range strings.Split(raw, "&") {
		k, v, _ := strings.Cut(pair, "=")
		if decode(k) == key {
			return decode(v)
		}
	}
	return ""
}

// CatalogSearchQuery preserves the source 256 UTF16 unit query limit,
// including a high surrogate when the last supplementary character is split.
func CatalogSearchQuery(value string) json.RawMessage {
	u := utf16.Encode([]rune(CatalogURLText(value)))
	if len(u) > 256 {
		u = u[:256]
	}
	return rawText(u)
}
