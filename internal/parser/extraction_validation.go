package parser

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"io"
	"itmap/internal/aiproviders"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const ExtractionSchemaVersion = 2
const ExtractionInputLimit = 120000

//go:embed extraction_schema.json
var extractionSchema []byte

func ExtractionSchema() json.RawMessage { return bytes.Clone(extractionSchema) }

type ExtractionCorrection struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}
type ExtractionNormalization struct {
	Data        map[string]any         `json:"data"`
	Corrections []ExtractionCorrection `json:"corrections"`
}

func extractionInvalid(message string) error {
	return &aiproviders.ServiceError{Code: "invalid_output", Message: message, Status: 422}
}
func utf16Length(s string) int { return len(utf16.Encode([]rune(s))) }
func NormalizeExtraction(content, raw string) (ExtractionNormalization, error) {
	return normalizeExtraction(content, raw, false)
}
func ValidateExtraction(content, raw string) (map[string]any, error) {
	v, e := normalizeExtraction(content, raw, true)
	return v.Data, e
}

type extractionField struct {
	Type any   `json:"type"`
	Enum []any `json:"enum"`
}

func normalizeExtraction(content, raw string, strict bool) (ExtractionNormalization, error) {
	var value any
	d := json.NewDecoder(strings.NewReader(content))
	d.UseNumber()
	if e := d.Decode(&value); e != nil {
		return ExtractionNormalization{}, extractionInvalid("Модель вернула некорректный JSON.")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ExtractionNormalization{}, extractionInvalid("Модель вернула некорректный JSON.")
	}
	var schema struct {
		Properties map[string]extractionField
		Required   []string
	}
	if json.Unmarshal(extractionSchema, &schema) != nil {
		return ExtractionNormalization{}, ErrSettingsStorage
	}
	v, ok := value.(map[string]any)
	if !ok || len(v) != len(schema.Required) {
		return ExtractionNormalization{}, extractionInvalid("Поля ответа не соответствуют схеме.")
	}
	for key := range v {
		if _, ok := schema.Properties[key]; !ok {
			return ExtractionNormalization{}, extractionInvalid("Поля ответа не соответствуют схеме.")
		}
	}
	// Порядок полей сохраняет первое source-сообщение при нескольких нарушениях.
	for _, key := range schema.Required {
		field := schema.Properties[key]
		x := v[key]
		kind := "null"
		valid := true
		switch a := x.(type) {
		case string:
			kind = "string"
			valid = jsStageTrim(a) != "" && !strings.ContainsRune(a, 0) && utf16Length(a) <= 150000
		case json.Number:
			kind = "number"
			n, e := a.Float64()
			valid = e == nil && !math.IsInf(n, 0) && !math.IsNaN(n) && n >= 0
			v[key] = n
		case nil:
		case bool:
			kind = "boolean"
		default:
			kind = "object"
		}
		permitted := false
		switch types := field.Type.(type) {
		case string:
			permitted = kind == types
		case []any:
			for _, t := range types {
				if t == kind {
					permitted = true
				}
			}
		}
		if field.Enum != nil {
			found := false
			for _, e := range field.Enum {
				if reflect.DeepEqual(e, v[key]) {
					found = true
				}
			}
			valid = valid && found
		}
		if !permitted || !valid {
			return ExtractionNormalization{}, extractionInvalid("Некорректное поле " + key + ".")
		}
	}
	if utf16Length(v["title"].(string)) > 500 || utf16Length(jsStageTrim(v["description_md"].(string))) < 100 {
		return ExtractionNormalization{}, extractionInvalid("Недостаточное содержательное описание.")
	}
	if v["salary_from"] != nil && v["salary_to"] != nil && v["salary_from"].(float64) > v["salary_to"].(float64) {
		return ExtractionNormalization{}, extractionInvalid("Границы зарплаты перепутаны.")
	}
	normalized := stageLegacyCaseText(stageUTF16RegexText(compactSalaryDigits(cases.Lower(language.Und).String(raw))))
	salaryText := strings.Join(regexp.MustCompile(`(?i)[^\n]{0,150}(?:зарплат|оклад|salary|руб|доллар|евро|₽|\$|usd|eur|€)[^\n]{0,150}`).FindAllString(normalized, -1), "\n")
	for _, key := range []string{"salary_from", "salary_to"} {
		if v[key] == nil {
			continue
		}
		n := v[key].(float64)
		literal := regexp.QuoteMeta(jsStageNumber(n))
		short := strings.Replace(jsStageNumber(n/1000), ".", "[.,]", 1)
		a := regexp.MustCompile(`(?:^|[^0-9])` + literal + `(?:$|[^0-9])`)
		b, e := regexp.Compile(`(?:^|[^0-9])` + short + jsStageSpace + `*(?:тыс|k\b)`)
		if !a.MatchString(salaryText) && (e != nil || !b.MatchString(salaryText)) {
			return ExtractionNormalization{}, extractionInvalid("Зарплата не подтверждается исходным текстом.")
		}
	}
	out := ExtractionNormalization{Data: v, Corrections: []ExtractionCorrection{}}
	unverified := func(key, message string) error {
		if strict {
			return extractionInvalid(message)
		}
		out.Corrections = append(out.Corrections, ExtractionCorrection{key, message})
		v[key] = nil
		return nil
	}
	evidence := []struct {
		key      string
		patterns map[string]string
	}{
		{"salary_currency", map[string]string{"RUB": `руб|₽|\brub\b`, "USD": `доллар|\$|\busd\b`, "EUR": `евро|€|\beur\b`}},
		{"salary_tax_type", map[string]string{"gross": `до (?:вычета )?налог|gross|before tax`, "net": `на руки|после (?:вычета )?налог|net|after tax`}},
		{"salary_period", map[string]string{"month": `месяц|month|/мес`, "year": `год|year|annual`, "hour": `час|hour|hourly`}},
		{"work_format", map[string]string{"office": `офис|office|on[^\n\r\x{2028}\x{2029}]site`, "hybrid": `гибрид|hybrid`, "remote": `удал[её]н|remote|дистанцион`}},
	}
	for _, rule := range evidence {
		if v[rule.key] == nil {
			continue
		}
		text := v[rule.key].(string)
		pattern := rule.patterns[text]
		if pattern != "" && !regexp.MustCompile("(?i)"+pattern).MatchString(stageLegacyCaseText(raw)) {
			if e := unverified(rule.key, "Значение "+rule.key+" не подтверждается исходным текстом."); e != nil {
				return ExtractionNormalization{}, e
			}
		} else if rule.key == "salary_currency" && pattern == "" && !strings.Contains(cases.Lower(language.Und).String(raw), cases.Lower(language.Und).String(text)) {
			if e := unverified(rule.key, "Валюта не подтверждена."); e != nil {
				return ExtractionNormalization{}, e
			}
		}
	}
	if v["published_at"] != nil {
		date := v["published_at"].(string)
		_, e := time.Parse("2006-01-02", date)
		if !regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`).MatchString(date) || e != nil {
			return ExtractionNormalization{}, extractionInvalid("Дата публикации должна быть действительной датой YYYY-MM-DD.")
		}
		y, m, day := date[:4], date[5:7], date[8:]
		month, _ := strconv.Atoi(m)
		dayNumber, _ := strconv.Atoi(day)
		months := []string{"январ", "феврал", "март", "апрел", "мая", "июн", "июл", "август", "сентябр", "октябр", "ноябр", "декабр"}
		human := regexp.MustCompile(`(?i)\b` + strconv.Itoa(dayNumber) + jsStageSpace + `+` + months[month-1] + `[^\n]{0,12}` + y)
		if !strings.Contains(raw, date) && !strings.Contains(raw, day+"."+m+"."+y) && !human.MatchString(stageUTF16RegexText(stageLegacyCaseText(raw))) {
			if e := unverified("published_at", "Дата публикации не подтверждается источником."); e != nil {
				return ExtractionNormalization{}, e
			}
		}
	}
	return out, nil
}
func compactSalaryDigits(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i := 0; i < len(r); {
		if i > 0 && r[i-1] >= '0' && r[i-1] <= '9' && jsStageTrim(string(r[i])) == "" {
			j := i
			for j < len(r) && jsStageTrim(string(r[j])) == "" {
				j++
			}
			if j < len(r) && r[j] >= '0' && r[j] <= '9' {
				i = j
				continue
			}
		}
		b.WriteRune(r[i])
		i++
	}
	return b.String()
}
func jsStageNumber(n float64) string {
	if n == 0 {
		return "0"
	}
	if n >= 1e-6 && n < 1e21 {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	s := strconv.FormatFloat(n, 'g', -1, 64)
	return regexp.MustCompile(`e([+-])0([0-9])`).ReplaceAllString(s, "e${1}${2}")
}

const jsStageSpace = `[\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// JS regex без флага u считает UTF-16 units в ограничениях длины окна.
func stageUTF16RegexText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 0xffff {
			b.WriteRune('\ufffd')
			b.WriteRune('\ufffd')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Legacy /i без u не сводит non-ASCII long-s/Kelvin к ASCII S/K.
func stageLegacyCaseText(s string) string {
	return strings.NewReplacer("ſ", "\ufffd", "K", "\ufffd").Replace(s)
}
