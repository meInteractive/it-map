package applications

import "strings"

// legacyWhitespace matches ECMAScript WhiteSpace and LineTerminator, used by
// String.trim and regexp \s. Go's unicode.IsSpace differs: it includes NEL
// (U+0085), but excludes BOM (U+FEFF). Either difference changes receipt hashes.
func legacyWhitespace(r rune) bool {
	if r >= '\u2000' && r <= '\u200a' {
		return true
	}
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680',
		'\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return false
}

func trimLegacyText(s string) string {
	return strings.TrimFunc(s, legacyWhitespace)
}

// Legacy JSON represents IDs as JavaScript Numbers. Larger int64 values cannot
// identify the same vacancy reliably across the old/new API boundary.
const maxLegacyInteger int64 = 1<<53 - 1
