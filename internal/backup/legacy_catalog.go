package backup

// The unsigned v14–15 path calls validateBackupPayload before full layout/FK
// validation. Modern signed formats deliberately do not inherit these checks.
import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

func jsNullish(v *jsonValue) bool { return v == nil || v.kind == 'n' }

// String(value) and Number(value) are needed because old agency ratings permit
// numeric strings, booleans and single-element arrays. JSON encoding is not
// JavaScript coercion (notably [] -> "", [null] -> "", {} -> NaN).
func legacyString(v *jsonValue) []uint16 {
	if v == nil {
		return utf16.Encode([]rune("undefined"))
	}
	if v.kind == 's' {
		return v.text
	}
	if v.kind == '{' {
		return utf16.Encode([]rune("[object Object]"))
	}
	if v.kind == '[' {
		out := []uint16{}
		for i, item := range v.items {
			if i > 0 {
				out = append(out, ',')
			}
			if !jsNullish(item) {
				out = append(out, legacyString(item)...)
			}
		}
		return out
	}
	if v.kind == 'f' && math.IsInf(v.number, 0) {
		if v.number < 0 {
			return utf16.Encode([]rune("-Infinity"))
		}
		return utf16.Encode([]rune("Infinity"))
	}
	return utf16.Encode([]rune(string(rawValue(v))))
}

var legacyDecimal = regexp.MustCompile(`^[+-]?(?:(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?|Infinity)$`)

func legacyNumber(v *jsonValue) float64 {
	if v == nil {
		return math.NaN()
	}
	switch v.kind {
	case 'n':
		return 0
	case 'f':
		return v.number
	case 'b':
		if v.boolean {
			return 1
		}
		return 0
	}
	s := stringFromUnits(trimUnits(legacyString(v)))
	if s == "" {
		return 0
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
			// Explicit digits exclude signs/underscores accepted by Go's parsers.
			for _, c := range s[2:] {
				digit := strings.IndexRune("0123456789abcdef", c)
				if c >= 'A' && c <= 'F' {
					digit = int(c-'A') + 10
				}
				if digit < 0 || digit >= base {
					return math.NaN()
				}
			}
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	if !legacyDecimal.MatchString(s) {
		return math.NaN()
	}
	if s == "Infinity" || s == "+Infinity" {
		return math.Inf(1)
	}
	if s == "-Infinity" {
		return math.Inf(-1)
	}
	n, _ := strconv.ParseFloat(s, 64)
	return n
}

func legacyRating(v *jsonValue, max float64, halfStep, integer bool) bool {
	if jsNullish(v) {
		return true
	}
	if len(trimUnits(legacyString(v))) == 0 {
		return false
	}
	n := legacyNumber(v)
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= max &&
		(!halfStep || math.Abs(n*2-math.Floor(n*2+0.5)) <= 1e-9) && (!integer || math.Trunc(n) == n)
}

var legacyReviewKeys = []string{"habr", "dreamJob", "glassDoor", "superJob", "pravdaSotrudnikov", "nahjob", "bossNadzor", "yandexMap"}
var legacyAgencyRatings = [][2]string{
	{"ethics", "Этика"}, {"vacancyHonesty", "Честность вакансий"}, {"feedback", "Обратная связь"}, {"vacancyRelevance", "Релевантность вакансий"},
	{"itExpertise", "IT-экспертиза"}, {"clientKnowledge", "Знание клиента"}, {"processOrganization", "Организация процесса"}, {"candidateSupport", "Поддержка кандидата"},
}

func validateLegacyCatalog(catalog *jsonValue, version int) error {
	companies := catalog.field("companies")
	if companies == nil || companies.kind != '[' {
		return invalid("В бэкапе отсутствует корректный список компаний.")
	}
	for _, c := range companies.items {
		name := c.field("name")
		if name == nil || name.kind != 's' || len(trimUnits(name.text)) == 0 {
			return invalid("В бэкапе отсутствует корректный список компаний.")
		}
	}
	for _, c := range companies.items {
		score, evidence := c.field("aiScore"), c.field("aiEvidence")
		if version >= 15 && (score == nil || evidence == nil) {
			return invalid("В бэкапе отсутствуют поля оценки ИИ компании.")
		}
		if !jsNullish(score) && (score.kind != 'f' || score.number != 0 && score.number != 5 && score.number != 10) {
			return invalid("Балл ИИ в бэкапе: допустимы только 0, 5, 10 или null.")
		}
		if evidence != nil && (evidence.kind != 's' || len(evidence.text) > 10000) {
			return invalid("Обоснование ИИ в бэкапе должно быть текстом до 10000 символов.")
		}
		if legacyNumber(score) > 0 && (evidence == nil || len(trimUnits(evidence.text)) == 0) {
			return invalid("Для баллов за ИИ в бэкапе укажите обоснование и источники.")
		}
		sources, reviews := c.field("reviewSources"), c.field("reviews")
		for _, key := range legacyReviewKeys {
			v := sources.field(key)
			if v == nil || v.kind != 's' {
				return invalid("В бэкапе отсутствуют источники отзывов компаний.")
			}
		}
		if reviews == nil || reviews.kind != '{' && reviews.kind != '[' {
			return invalid("В бэкапе отсутствуют агрегированные отзывы компаний.")
		}
		for _, spec := range []struct {
			key, label string
			max        int
		}{{"plusShort", "Plus Short", 512}, {"minusShort", "Minus Short", 512}, {"plus", "Plus", 2000}, {"minus", "Minus", 2000}} {
			v := reviews.field(spec.key)
			if !jsNullish(v) && v.kind != 's' {
				return invalid(spec.label + ": в бэкапе должно быть текстовое значение.")
			}
			if v != nil && v.kind == 's' && len(v.text) > spec.max {
				return invalid(spec.label + ": значение в бэкапе длиннее " + strconv.Itoa(spec.max) + " символов.")
			}
		}
	}
	if agencies := catalog.field("agencies"); agencies != nil && agencies.kind == '[' {
		for _, a := range agencies.items {
			for _, spec := range legacyAgencyRatings {
				if !legacyRating(a.field(spec[0]), 10, true, false) {
					return invalid(spec[1] + ": в бэкапе должна быть оценка от 0 до 10 с шагом 0,5 или NULL.")
				}
			}
			for _, spec := range []struct {
				key, label string
				max        float64
				integer    bool
			}{{"reviewsScore", "Балл отзывов", 80, false}, {"relevanceScore", "Балл релевантности", 10, true}, {"revenueScore", "Балл выручки", 10, true}} {
				if !legacyRating(a.field(spec.key), spec.max, false, spec.integer) {
					return invalid(spec.label + ": в бэкапе содержится некорректное значение.")
				}
			}
			for _, key := range []string{"twoGis", "otzovik", "personAgency", "yell"} {
				v := a.field("reviewSources").field(key)
				if v == nil || v.kind != 's' {
					return invalid("В бэкапе отсутствуют новые источники отзывов агентств.")
				}
			}
		}
	}
	return nil
}
