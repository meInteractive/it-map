package backup

// JSON.stringify preserves object insertion order except for array-index keys.
// It also preserves lone UTF-16 surrogates as escapes. A map[string]any roundtrip
// loses both properties, so integrity uses this small ordered JSON tree.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type jsonMember struct {
	key   []uint16
	value *jsonValue
}
type jsonValue struct {
	kind    byte
	text    []uint16
	number  float64
	boolean bool
	items   []*jsonValue
	members []jsonMember
}

func stringFromUnits(units []uint16) string { return string(utf16.Decode(units)) }

func textField(v *jsonValue, name string) string {
	f := v.field(name)
	if f == nil || f.kind != 's' {
		return ""
	}
	return stringFromUnits(f.text)
}
func rawValue(v *jsonValue) json.RawMessage {
	if v == nil {
		return nil
	}
	return v.stringify(nil, false)
}

// A SQL JSON string may itself contain a lone UTF-16 unit. JSON.parse accepts
// it within a string literal. Preserve that unit as an escape before Go's
// UTF-8 parser sees the nested document; an invalid escape stays invalid.
func jsonSourceText(units []uint16) ([]byte, error) {
	var out []byte
	inside, escaped := false, false
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xd800 && u <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			out = utf8.AppendRune(out, utf16.DecodeRune(rune(u), rune(units[i+1])))
			i++
			escaped = false
			continue
		}
		if u >= 0xd800 && u <= 0xdfff {
			if !inside || escaped {
				return nil, invalid("Некорректный JSON каталога.")
			}
			quoted := appendJSONString(nil, []uint16{u})
			out = append(out, quoted[1:len(quoted)-1]...)
			continue
		}
		if u == '"' && !escaped {
			inside = !inside
		}
		if inside && u == '\\' && !escaped {
			escaped = true
		} else {
			escaped = false
		}
		out = utf8.AppendRune(out, rune(u))
	}
	return out, nil
}

func stringUnits(raw []byte) []uint16 {
	var units []uint16
	for i := 1; i < len(raw)-1; {
		if raw[i] != '\\' {
			r, n := utf8.DecodeRune(raw[i:])
			i += n
			if r > 0xffff {
				hi, lo := utf16.EncodeRune(r)
				units = append(units, uint16(hi), uint16(lo))
			} else {
				units = append(units, uint16(r))
			}
			continue
		}
		i++
		c := raw[i]
		i++
		if c == 'u' {
			n, _ := strconv.ParseUint(string(raw[i:i+4]), 16, 16)
			units = append(units, uint16(n))
			i += 4
			continue
		}
		switch c {
		case 'b':
			c = '\b'
		case 'f':
			c = '\f'
		case 'n':
			c = '\n'
		case 'r':
			c = '\r'
		case 't':
			c = '\t'
		}
		units = append(units, uint16(c))
	}
	return units
}
func tokenString(data []byte, start, end int64) []uint16 {
	part := data[start:end]
	// Decoder offsets precede separators/whitespace; JSON syntax was validated.
	return stringUnits(part[bytes.IndexByte(part, '"'):])
}
func orderedJSON(data []byte) (*jsonValue, error) {
	if !json.Valid(data) || !utf8.Valid(data) {
		return nil, invalid("Некорректный JSON бэкапа.")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return readJSONValue(d, data)
}
func readJSONValue(d *json.Decoder, data []byte) (*jsonValue, error) {
	start := d.InputOffset()
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	v := &jsonValue{}
	switch t := token.(type) {
	case nil:
		v.kind = 'n'
	case bool:
		v.kind = 'b'
		v.boolean = t
	case string:
		v.kind = 's'
		v.text = tokenString(data, start, d.InputOffset())
	case json.Number:
		v.kind = 'f'
		v.number, _ = strconv.ParseFloat(string(t), 64)
	case json.Delim:
		v.kind = byte(t)
		if t == '[' {
			for d.More() {
				child, e := readJSONValue(d, data)
				if e != nil {
					return nil, e
				}
				v.items = append(v.items, child)
			}
		} else if t == '{' {
			seen := map[string]int{}
			for d.More() {
				before := d.InputOffset()
				if _, e := d.Token(); e != nil {
					return nil, e
				}
				key := tokenString(data, before, d.InputOffset())
				child, e := readJSONValue(d, data)
				if e != nil {
					return nil, e
				}
				encoded := string(appendJSONString(nil, key))
				if i, ok := seen[encoded]; ok {
					v.members[i].value = child
				} else {
					seen[encoded] = len(v.members)
					v.members = append(v.members, jsonMember{key, child})
				}
			}
			// ECMAScript OwnPropertyKeys: canonical uint32 indices first, except 2^32-1.
			sort.SliceStable(v.members, func(i, j int) bool {
				a, aok := arrayIndex(v.members[i].key)
				b, bok := arrayIndex(v.members[j].key)
				if aok != bok {
					return aok
				}
				return aok && a < b
			})
		}
		if _, err = d.Token(); err != nil {
			return nil, err
		}
	}
	return v, nil
}
func arrayIndex(key []uint16) (uint64, bool) {
	if len(key) == 0 || len(key) > 10 || len(key) > 1 && key[0] == '0' {
		return 0, false
	}
	var n uint64
	for _, u := range key {
		if u < '0' || u > '9' {
			return 0, false
		}
		n = n*10 + uint64(u-'0')
	}
	return n, n < 4294967295
}
func appendJSONString(out []byte, units []uint16) []byte {
	const digits = "0123456789abcdef"
	out = append(out, '"')
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch u {
		case '"', '\\':
			out = append(out, '\\', byte(u))
			continue
		case '\b':
			out = append(out, '\\', 'b')
			continue
		case '\f':
			out = append(out, '\\', 'f')
			continue
		case '\n':
			out = append(out, '\\', 'n')
			continue
		case '\r':
			out = append(out, '\\', 'r')
			continue
		case '\t':
			out = append(out, '\\', 't')
			continue
		}
		if u >= 0xd800 && u <= 0xdbff && i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
			out = utf8.AppendRune(out, utf16.DecodeRune(rune(u), rune(units[i+1])))
			i++
			continue
		}
		if u < 0x20 || u >= 0xd800 && u <= 0xdfff {
			out = append(out, '\\', 'u', digits[u>>12], digits[(u>>8)&15], digits[(u>>4)&15], digits[u&15])
			continue
		}
		out = utf8.AppendRune(out, rune(u))
	}
	return append(out, '"')
}
func keyIs(key []uint16, name string) bool {
	if len(key) != len(name) {
		return false
	}
	for i, u := range key {
		if u != uint16(name[i]) {
			return false
		}
	}
	return true
}
func (v *jsonValue) field(name string) *jsonValue {
	if v == nil || v.kind != '{' {
		return nil
	}
	for _, m := range v.members {
		if keyIs(m.key, name) {
			return m.value
		}
	}
	return nil
}
func (v *jsonValue) stringify(out []byte, omitIntegrity bool) []byte {
	switch v.kind {
	case 'n':
		return append(out, "null"...)
	case 'b':
		return strconv.AppendBool(out, v.boolean)
	case 's':
		return appendJSONString(out, v.text)
	case 'f':
		if math.IsInf(v.number, 0) || math.IsNaN(v.number) {
			return append(out, "null"...)
		}
		if v.number == 0 {
			return append(out, '0')
		}
		// encoding/json uses ECMAScript's fixed/exponent thresholds and removes
		// exponent padding; strconv's shortest float conversion supplies digits.
		n, _ := json.Marshal(v.number)
		return append(out, n...)
	case '[':
		out = append(out, '[')
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			out = item.stringify(out, false)
		}
		return append(out, ']')
	case '{':
		out = append(out, '{')
		first := true
		for _, m := range v.members {
			if omitIntegrity && keyIs(m.key, "integrity") {
				continue
			}
			if !first {
				out = append(out, ',')
			}
			first = false
			out = appendJSONString(out, m.key)
			out = append(out, ':')
			out = m.value.stringify(out, false)
		}
		return append(out, '}')
	}
	return out
}
func digest(v *jsonValue) string {
	sum := sha256.Sum256(v.stringify(nil, true))
	return hex.EncodeToString(sum[:])
}

// DigestJSON hashes the parsed object after removing its top-level integrity
// property, exactly as the source backupDigest/JSON.stringify does.
func DigestJSON(data []byte) (string, error) {
	v, err := orderedJSON(data)
	if err != nil {
		return "", err
	}
	if v.kind != '{' {
		return "", invalid("Некорректный объект бэкапа.")
	}
	return digest(v), nil
}

// ReadRequest enforces both declared and streamed byte limits before parsing.
// A UTF-8 BOM is consumed once, matching the legacy TextDecoder.
func ReadRequest(r io.Reader, declaredLength int64) ([]byte, error) {
	if declaredLength > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Файл бэкапа больше допустимых 25 МБ."}
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, invalid("Не удалось прочитать файл бэкапа.")
	}
	if len(data) > MaxBytes {
		return nil, &InputError{Status: 413, Message: "Файл бэкапа больше допустимых 25 МБ."}
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	data = decodeUTF8(data)
	if !json.Valid(data) {
		return nil, invalid("Некорректный JSON бэкапа.")
	}
	return data, nil
}

// WHATWG TextDecoder replaces a malformed sequence's valid prefix once,
// reprocessing its first invalid continuation byte as the next sequence.
func decodeUTF8(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	out := make([]byte, 0, len(data))
	for i := 0; i < len(data); {
		r, n := utf8.DecodeRune(data[i:])
		if r != utf8.RuneError || n != 1 {
			out = append(out, data[i:i+n]...)
			i += n
			continue
		}
		want := 1
		lead := data[i]
		switch {
		case lead >= 0xc2 && lead <= 0xdf:
			want = 2
		case lead >= 0xe0 && lead <= 0xef:
			want = 3
		case lead >= 0xf0 && lead <= 0xf4:
			want = 4
		}
		consumed := 1
		for j := 1; j < want && i+j < len(data); j++ {
			b := data[i+j]
			if b < 0x80 || b > 0xbf {
				break
			}
			if j == 1 && (lead == 0xe0 && b < 0xa0 || lead == 0xed && b > 0x9f || lead == 0xf0 && b < 0x90 || lead == 0xf4 && b > 0x8f) {
				break
			}
			consumed++
		}
		out = utf8.AppendRune(out, utf8.RuneError)
		i += consumed
	}
	return out
}

// JS trim includes FEFF in addition to Go's Unicode whitespace.
func trimName(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return strings.TrimSpace(string(r)) == "" || r == 0xfeff })
}
