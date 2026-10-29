package parser

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"itmap/internal/backup"
)

// Алгоритм SUM повторяет код общественного достояния SQLite3.53.3, src/func.c:
// https://github.com/sqlite/sqlite/blob/version-3.53.3/src/func.c
// Сохраняем целый аккумулятор, переполнение и компенсацию Kahan–Babuška–Neumaier.
type dashboardSum struct {
	count                 int64
	integer               int64
	approximate, overflow bool
	sum, correction       float64
}

func (s *dashboardSum) step(n float64) {
	previous := s.sum
	next := previous + n
	if math.Abs(previous) > math.Abs(n) {
		s.correction += (previous - next) + n
	} else {
		s.correction += (n - next) + previous
	}
	s.sum = next
}
func (s *dashboardSum) initInteger(n int64) {
	if n <= -4503599627370496 || n >= 4503599627370496 {
		small := n % 16384
		s.sum = float64(n - small)
		s.correction = float64(small)
	} else {
		s.sum = float64(n)
		s.correction = 0
	}
}
func (s *dashboardSum) stepInteger(n int64) {
	if n <= -4503599627370496 || n >= 4503599627370496 {
		small := n % 16384
		s.step(float64(n - small))
		s.step(float64(small))
	} else {
		s.step(float64(n))
	}
}
func (s *dashboardSum) add(integer bool, n int64, real float64) {
	s.count++
	if !s.approximate {
		if !integer {
			s.initInteger(s.integer)
			s.approximate = true
			s.step(real)
			return
		}
		if n > 0 && s.integer > math.MaxInt64-n || n < 0 && s.integer < math.MinInt64-n {
			s.overflow = true
			s.approximate = true
			s.initInteger(s.integer)
			s.stepInteger(n)
		} else {
			s.integer += n
		}
		return
	}
	if integer {
		s.stepInteger(n)
	} else {
		s.overflow = false
		s.step(real)
	}
}

// Числовая аффинность SQLite ограничена пробельными символами ASCII и завершается на NUL.
// Нецелая или неполная строка участвует в SUM как real с числовым префиксом.
func dashboardTextNumber(text string) (bool, int64, float64, error) {
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	text = strings.Trim(text, " \t\n\r\v\f")
	if n, e := strconv.ParseInt(text, 10, 64); e == nil {
		return true, n, 0, nil
	}
	i, digits := 0, 0
	if i < len(text) && (text[i] == '+' || text[i] == '-') {
		i++
	}
	for i < len(text) && text[i] >= '0' && text[i] <= '9' {
		i++
		digits++
	}
	if i < len(text) && text[i] == '.' {
		i++
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return false, 0, 0, nil
	}
	if i < len(text) && (text[i] == 'e' || text[i] == 'E') {
		end := i + 1
		if end < len(text) && (text[end] == '+' || text[end] == '-') {
			end++
		}
		first := end
		for end < len(text) && text[end] >= '0' && text[end] <= '9' {
			end++
		}
		if end > first {
			i = end
		}
	}
	prefix := strings.TrimPrefix(text[:i], "+")
	n, e := sqliteDecimal(prefix)
	if e != nil && !math.IsInf(n, 0) && n != 0 {
		return false, 0, 0, e
	}
	return false, 0, n, nil
}

// raw — скаляр JSON_EXTRACT, уже выбранный по первому совпадению ключа.
func (s *dashboardSum) addJSON(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return ErrDashboardStorage
	}
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		return nil
	}
	switch raw[0] {
	case 't':
		s.add(true, 1, 0)
	case 'f':
		s.add(true, 0, 0)
	case '{', '[':
		s.add(false, 0, 0)
	case '"':
		text, e := jsonSQLString(raw)
		if e != nil {
			return e
		}
		integer, n, real, e := dashboardTextNumber(text)
		if e != nil {
			return e
		}
		s.add(integer, n, real)
	default:
		text := string(raw)
		if !strings.ContainsAny(text, ".eE") {
			if n, e := strconv.ParseInt(text, 10, 64); e == nil {
				s.add(true, n, 0)
				return nil
			}
		}
		n, e := sqliteDecimal(text)
		if e != nil && !math.IsInf(n, 0) && n != 0 {
			return e
		}
		s.add(false, 0, n)
	}
	return nil
}
func (s *dashboardSum) sqlValue() (any, error) {
	if s.count == 0 {
		return nil, nil
	}
	if !s.approximate {
		return s.integer, nil
	}
	if s.overflow {
		return nil, ErrDashboardStorage
	}
	n := s.sum
	if !math.IsNaN(s.correction) && !math.IsInf(s.correction, 0) {
		n += s.correction
	}
	if math.IsNaN(n) {
		return nil, nil
	} // SQLite представляет NaN как SQL NULL.
	return n, nil
}
func (s *dashboardSum) jsonValue(coalesce bool) (json.RawMessage, error) {
	value, e := s.sqlValue()
	if e != nil {
		return nil, e
	}
	if value == nil && coalesce {
		value = 0
	}
	if n, ok := value.(float64); ok && math.IsInf(n, 0) {
		return json.RawMessage("null"), nil
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	return backup.StringifyParsedJSON(raw)
}
func (s *dashboardSum) coalescedJSON() (json.RawMessage, error) { return s.jsonValue(true) }
