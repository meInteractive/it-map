package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

var ErrJobInput = errors.New("invalid scheduler job input")

// jsonPathFirst follows SQLite JSON1's first duplicate member, rather than
// JSON.parse's last-wins object behavior. Payload/cursor are stored JSON text.
func jsonPathFirst(raw []byte, path []string) ([]byte, error) {
	if !json.Valid(raw) {
		return nil, ErrJobInput
	}
	if len(path) == 0 {
		return raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, e := dec.Token()
	if e != nil {
		return nil, ErrJobInput
	}
	if token != json.Delim('{') {
		return nil, nil
	}
	for dec.More() {
		name, e := dec.Token()
		if e != nil {
			return nil, ErrJobInput
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return nil, ErrJobInput
		}
		if name == path[0] {
			return jsonPathFirst(value, path[1:])
		}
	}
	return nil, nil
}
func sqliteJSONText(raw []byte, path ...string) (string, bool, error) {
	value, e := jsonPathFirst(raw, path)
	if e != nil {
		return "", false, e
	}
	value = bytes.TrimSpace(value)
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return "", false, nil
	}
	switch value[0] {
	case '"':
		text, e := jsonSQLString(value)
		return text, true, e
	case 't':
		return "1", true, nil
	case 'f':
		return "0", true, nil
	case '{', '[':
		var compact bytes.Buffer
		if json.Compact(&compact, value) != nil {
			return "", false, ErrJobInput
		}
		return compact.String(), true, nil
	}
	text := string(value)
	if !strings.ContainsAny(text, ".eE") {
		if n, e := strconv.ParseInt(text, 10, 64); e == nil {
			return strconv.FormatInt(n, 10), true, nil
		}
	}
	n, e := sqliteDecimal(text)
	if math.IsInf(n, 1) {
		return "Inf", true, nil
	}
	if math.IsInf(n, -1) {
		return "-Inf", true, nil
	}
	if n == 0 && (e == nil || errors.Is(e, strconv.ErrRange)) {
		return "0.0", true, nil
	}
	if e != nil || math.IsNaN(n) {
		return "", false, ErrJobInput
	}
	return sqliteRealText(n), true, nil

}

// Preserve escaped lone UTF16 as SQLite's UTF8 surrogate bytes. Replacing it
// with U+FFFD would incorrectly match a different provider operation key.
func jsonSQLString(raw []byte) (string, error) {
	out := []byte{}
	for i := 1; i < len(raw)-1; i++ {
		b := raw[i]
		if b != '\\' {
			out = append(out, b)
			continue
		}
		i++
		switch raw[i] {
		case '"', '\\', '/':
			out = append(out, raw[i])
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		case 'u':
			cp, e := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
			if e != nil {
				return "", ErrJobInput
			}
			i += 4
			if cp >= 0xd800 && cp <= 0xdbff && i+6 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
				low, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
				if e == nil && low >= 0xdc00 && low <= 0xdfff {
					cp = 0x10000 + (cp-0xd800)*1024 + low - 0xdc00
					i += 6
				}
			}
			if cp >= 0xd800 && cp <= 0xdfff {
				out = append(out, byte(0xe0|cp>>12), byte(0x80|(cp>>6)&63), byte(0x80|cp&63))
			} else {
				out = utf8.AppendRune(out, rune(cp))
			}
		default:
			return "", ErrJobInput
		}
	}
	return string(out), nil
}

// SQLite 3.53's %!.17g trims only the documented 00/99 round-trip runs,
// rather than always selecting the shortest Ryu representation. Derived from
// public-domain sqlite3FpDecode (sqlite/sqlite src/util.c); no C/SQLite runtime.
func sqliteRealText(number float64) string {
	sign := ""
	if number < 0 {
		sign = "-"
		number = -number
	}
	encoded := math.Float64bits(number)
	binaryExponent := int((encoded >> 52) & 0x7ff)
	mantissa := encoded & 0x000fffffffffffff
	if binaryExponent == 0 {
		shift := bits.LeadingZeros64(mantissa)
		mantissa <<= shift
		binaryExponent = -1074 - shift
	} else {
		mantissa = mantissa<<11 | uint64(1)<<63
		binaryExponent -= 1086
	}
	power := 17 - ((binaryExponent + 63) * 78913 >> 18)
	hi, _ := bits.Mul64(mantissa, sqlitePowerOfTen()[power])
	hi >>= uint(-(binaryExponent + (power * 108853 >> 15) + 2))
	decimal := (hi + ((hi << 1) & 2)) >> 1
	digits := []byte(strconv.FormatUint(decimal, 10))
	point := len(digits) - power
	precision := 17
	if digits[15] == '9' && digits[14] == '9' {
		j := 14
		for j > 0 && digits[j-1] == '9' {
			j--
		}
		prefix := uint64(1)
		if j > 0 {
			prefix, _ = strconv.ParseUint(string(digits[:j]), 10, 64)
			prefix++
		}
		candidate, _ := strconv.ParseFloat(strconv.FormatUint(prefix, 10)+"e"+strconv.Itoa(point-j), 64)
		if candidate == number {
			precision = j + 1
		}
	} else if point >= len(digits) || (digits[15] == '0' && digits[14] == '0' && digits[13] == '0') {
		j := 13
		for j > 1 && digits[j-1] == '0' {
			j--
		}
		candidate, _ := strconv.ParseFloat(string(digits[:j])+"e"+strconv.Itoa(point-j), 64)
		if candidate == number {
			precision = j + 1
		}
	}
	roundUp := digits[precision] >= '5'
	digits = digits[:precision]
	if roundUp {
		for j := len(digits) - 1; j >= 0; j-- {
			digits[j]++
			if digits[j] <= '9' {
				break
			}
			digits[j] = '0'
			if j == 0 {
				digits = append([]byte{'1'}, digits...)
				point++
			}
		}
	}
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
	}
	exponent := point - 1
	if exponent < -4 || exponent >= 17 {
		mantissa := string(digits[:1]) + "."
		if len(digits) > 1 {
			mantissa += string(digits[1:])
		} else {
			mantissa += "0"
		}
		direction := "+"
		if exponent < 0 {
			direction = "-"
			exponent = -exponent
		}
		exp := strconv.Itoa(exponent)
		if len(exp) < 2 {
			exp = "0" + exp
		}
		return sign + mantissa + "e" + direction + exp
	}
	if point <= 0 {
		return sign + "0." + strings.Repeat("0", -point) + string(digits)
	}
	if point >= len(digits) {
		return sign + string(digits) + strings.Repeat("0", point-len(digits)) + ".0"
	}
	return sign + string(digits[:point]) + "." + string(digits[point:])
}

// Top 64 normalized bits of 10^p, as specified by SQLite powerOfTen. Deriving
// them with integers avoids platform libm and the intermediate double-rounding
// introduced by formatting a binary float to 18 decimal digits first.
var sqlitePowerOfTen = sync.OnceValue(func() map[int]uint64 {
	powers := map[int]uint64{}
	for p := -348; p <= 347; p++ {
		exponent := p
		if exponent < 0 {
			exponent = -exponent
		}
		n := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
		shift := 63 - (p * 108853 >> 15)
		if p < 0 {
			n.Quo(new(big.Int).Lsh(big.NewInt(1), uint(shift)), n)
		} else if shift >= 0 {
			n.Lsh(n, uint(shift))
		} else {
			n.Rsh(n, uint(-shift))
		}
		powers[p] = n.Uint64()
	}
	return powers
})

// SQLite AtoF retains the first uint64 significand (about 19 digits), including
// its intentional truncation of later decimal digits. JSON grammar is checked
// before this helper is called; no whitespace/non-decimal coercion is added.
func sqliteDecimal(text string) (float64, error) {
	negative := false
	i := 0
	if text[0] == '-' {
		negative = true
		i++
	}
	const threshold = (^uint64(0) - 9) / 10
	var significand uint64
	exponent := 0
	full := false
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		if full {
			exponent++
		} else {
			significand = significand*10 + uint64(text[i]-'0')
			full = significand >= threshold
		}
		i++
	}
	if i < len(text) && text[i] == '.' {
		i++
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			if significand < threshold {
				significand = significand*10 + uint64(text[i]-'0')
				exponent--
			}
			i++
		}
	}
	if i < len(text) && (text[i] == 'e' || text[i] == 'E') {
		i++
		sign := 1
		if text[i] == '-' {
			sign = -1
			i++
		} else if text[i] == '+' {
			i++
		}
		extra := 0
		for i < len(text) {
			if extra < 10000 {
				extra = extra*10 + int(text[i]-'0')
			} else {
				extra = 10000
			}
			i++
		}
		exponent += sign * extra
	}
	prefix := ""
	if negative {
		prefix = "-"
	}
	return strconv.ParseFloat(prefix+strconv.FormatUint(significand, 10)+"e"+strconv.Itoa(exponent), 64)
}
