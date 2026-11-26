package catalog

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"itmap/internal/backup"
)

type ImportRow map[string]json.RawMessage

func importText(row ImportRow, key string) string {
	var value string
	if json.Unmarshal(row[key], &value) == nil {
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(row[key], &number) == nil {
		return number.String()
	}
	return ""
}

func importKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func importNumber(value string, min, max float64, whole bool) (float64, error) {
	value = strings.ReplaceAll(strings.Join(strings.Fields(value), ""), ",", ".")
	result, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(result) || math.IsInf(result, 0) || result < min || result > max || whole && math.Trunc(result) != result {
		return 0, fmt.Errorf("недопустимое число")
	}
	return result, nil
}

func importID(row ImportRow, field string) (int64, error) {
	if text := importText(row, field); text != "" {
		id, err := importNumber(text, 1, 9007199254740991, true)
		if err != nil {
			return 0, fmt.Errorf("ID должен быть положительным целым числом")
		}
		return int64(id), nil
	}
	return 0, nil
}

func importURL(text string) (*url.URL, error) {
	u, err := url.Parse(text)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("ожидается ссылка http или https")
	}
	return u, nil
}

func importURLKey(text string) string {
	u, err := importURL(text)
	if err != nil {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	path := strings.TrimRight(strings.ToLower(u.Path), "/")
	if path == "" {
		path = "/"
	}
	return host + path
}

type importField struct {
	key, target string
	kind        string
}

func importFields(keys string, kind string) []importField {
	var fields []importField
	for _, key := range strings.Fields(keys) {
		input, target, mapped := strings.Cut(key, ":")
		if !mapped {
			target = input
		}
		fields = append(fields, importField{input, target, kind})
	}
	return fields
}

var importReviews = append(
	importFields("reviewHabr:reviewSources.habr reviewDreamJob:reviewSources.dreamJob reviewGlassDoor:reviewSources.glassDoor reviewSuperJob:reviewSources.superJob reviewPravdaSotrudnikov:reviewSources.pravdaSotrudnikov reviewNahjob:reviewSources.nahjob reviewBossNadzor:reviewSources.bossNadzor reviewYandexMap:reviewSources.yandexMap", "url"),
	importFields("reviewsPlusShort:reviews.plusShort reviewsMinusShort:reviews.minusShort reviewsPlus:reviews.plus reviewsMinus:reviews.minus", "text")...,
)

var importSources = importFields("companySite:vacancySources.companySite hh:vacancySources.hh habr:vacancySources.habr getmatch:vacancySources.getmatch telegram:vacancySources.telegram vacancyDreamJob:vacancySources.dreamJob hCareers:vacancySources.hCareers rabota:vacancySources.rabota", "url")

var importCompanyUpdates = [][]importField{
	importFields("wlb deferment mobilization aiEvidence housingStatus:housing.status housingForm:housing.form housingTerms:housing.terms housingAfter:housing.after housingSource:housing.source vpnAiSummary:vpnAi.summary vpn:vpnAi.vpn ai:vpnAi.ai chatgpt:vpnAi.chatgpt claude:vpnAi.claude grok:vpnAi.grok qwen:vpnAi.qwen deepseek:vpnAi.deepseek vpnAiData:vpnAi.data vpnAiEvidence:vpnAi.evidence vpnAiSource1:vpnAi.source1 vpnAiSource2:vpnAi.source2 vpnAiRecruiter:vpnAi.recruiter", "text"),
	importFields("rating wlbScore aiScore", "number"),
	importFields("vpnAiChecked:vpnAi.checked", "date"),
	importSources, importReviews,
}

var importRevenue = [][]importField{
	importFields("revenueAmount:revenue.amount revenueYear:revenue.year", "number"),
	importFields("revenueMetric:revenue.metric revenueLegal:revenue.legal revenueComposition:revenue.composition revenueCoverage:revenue.coverage revenueStatus:revenue.status revenueComment:revenue.comment", "text"),
	importFields("revenueInn:revenue.inn", "inn"),
	importFields("revenueSource1:revenue.source1 revenueSource2:revenue.source2", "url"),
}
var importEntityCreate = [][]importField{
	importFields("sector roles confidence risk", "text"),
	importFields("applicationEmail", "email"),
	importFields("applicationUrl", "url"),
}

var importAgencyFields = [][]importField{
	importFields("executiveSearch closedVacancies vacancyPublicity employerDisclosure salaryDisclosure primaryVacancyChannel publicSourcesQuality vacancyFreshness acceptsCv confidence", "choice"),
	importFields("workingGuide", "text"),
	importFields("agencyTypes functionalSpecializations positionLevels recommendedContactMethods", "list"),
	importFields("rank score ethics vacancyHonesty feedback vacancyRelevance itExpertise clientKnowledge processOrganization candidateSupport", "number"),
	importFields("reviewTwoGis:reviewSources.twoGis reviewOtzovik:reviewSources.otzovik reviewPersonAgency:reviewSources.personAgency reviewYell:reviewSources.yell", "url"),
	importSources[:5], importReviews,
}

func setImportedField(entity storedEntity, target string, value any) {
	group, key, nested := strings.Cut(target, ".")
	if !nested {
		entity[group] = value
		return
	}
	fields := maps.Clone(obj(entity[group]))
	if fields == nil {
		fields = map[string]any{}
	}
	fields[key] = value
	entity[group] = fields
}

func applyImportFields(entity storedEntity, row ImportRow, sets ...[]importField) (bool, error) {
	changed := false
	for _, specs := range sets {
		for _, field := range specs {
			text := importText(row, field.key)
			if text == "" {
				continue
			}
			if field.kind == "date" && len(row[field.key]) > 0 && row[field.key][0] != '"' {
				serial, err := importNumber(text, 1, 2958465, false)
				if err != nil {
					return false, fmt.Errorf("%s: некорректная дата Excel", field.key)
				}
				text = time.UnixMilli(int64(math.Round((serial - 25569) * 86400000))).UTC().Format("2006-01-02")
			}
			value, err := validateImportField(field, text)
			if err != nil {
				return false, fmt.Errorf("%s: %w", field.key, err)
			}
			setImportedField(entity, field.target, value)
			changed = true
		}
	}
	return changed, nil
}

func validateImportField(field importField, text string) (any, error) {
	if len(utf16.Encode([]rune(text))) > 10000 {
		return nil, fmt.Errorf("слишком длинное значение")
	}
	switch field.kind {
	case "url":
		if len(text) > 2048 {
			return nil, fmt.Errorf("слишком длинная ссылка")
		}
		_, err := importURL(text)
		return text, err
	case "email":
		for _, cell := range strings.Split(text, ";") {
			address := strings.TrimSpace(cell)
			parsed, err := mail.ParseAddress(address)
			if err != nil || parsed.Address != address || !strings.Contains(strings.SplitN(address, "@", 2)[1], ".") {
				return nil, fmt.Errorf("некорректный email")
			}
		}
	case "date":
		for _, format := range []string{"2006-01-02", "2.1.2006", "2/1/2006", "2-1-2006"} {
			if parsed, err := time.Parse(format, text); err == nil {
				return parsed.Format("2006-01-02"), nil
			}
		}
		return nil, fmt.Errorf("некорректная дата")
	case "inn":
		for _, item := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
			if (len(item) != 10 && len(item) != 12) || strings.Trim(item, "0123456789") != "" {
				return nil, fmt.Errorf("ИНН должен содержать 10 или 12 цифр")
			}
		}
	case "choice":
		return agencyImportChoice(field.key, text)
	case "list":
		var values []string
		for _, part := range strings.Split(text, ";") {
			if item := strings.TrimSpace(part); item != "" {
				value, err := agencyImportChoice(field.key, item)
				if err != nil {
					return nil, err
				}
				values = append(values, value)
			}
		}
		return values, nil
	case "number":
		switch field.key {
		case "rating":
			return importNumber(text, 0, 5, false)
		case "wlbScore":
			return importNumber(text, 0, 100, true)
		case "score":
			return importNumber(text, 0, 100, false)
		case "rank":
			return importNumber(text, 1, 9007199254740991, true)
		case "revenueYear":
			return importNumber(text, 1900, 2100, true)
		case "revenueAmount":
			return importNumber(text, 0, math.MaxFloat64, false)
		case "aiScore":
			n, err := importNumber(text, 0, 10, true)
			if err != nil || (n != 0 && n != 5 && n != 10) {
				return nil, fmt.Errorf("допустимы 0, 5 или 10")
			}
			return n, nil
		default:
			n, err := importNumber(text, 0, 10, false)
			if err != nil || math.Trunc(n*2) != n*2 {
				return nil, fmt.Errorf("допустимы оценки от 0 до 10 с шагом 0,5")
			}
			return n, nil
		}
	}
	return text, nil
}

func applyImportSalaries(entity storedEntity, row ImportRow, agency bool) (bool, error) {
	changed := false
	for _, level := range []string{"middle", "senior", "lead", "head"} {
		from, to := importText(row, level+"From"), importText(row, level+"To")
		if from == "" && to == "" {
			continue
		}
		pair := []any{nil, nil}
		if previous := asSlice(entity[level]); !agency && len(previous) == 2 {
			copy(pair, previous)
		}
		for i, text := range []string{from, to} {
			if text == "" {
				continue
			}
			n, err := importNumber(text, 0, 9007199254740991, true)
			if err != nil {
				return false, fmt.Errorf("%s: некорректная зарплата", level)
			}
			pair[i] = n
		}
		if a, ok := pair[0].(float64); ok {
			if b, ok := pair[1].(float64); ok && a > b {
				return false, fmt.Errorf("%s: зарплата «от» выше зарплаты «до»", level)
			}
		}
		entity[level] = pair
		if agency {
			setImportedField(entity, "salaryRangeSource."+level, "manual")
		}
		changed = true
	}
	return changed, nil
}

// Таблица вариантов общая с нормализацией каталога.
func agencyImportChoice(field, value string) (string, error) {
	options := backup.CatalogAgencyOptions(field)
	if field == "confidence" {
		options = []string{"Высокая", "Средняя", "Низкая"}
	}
	for _, allowed := range options {
		if strings.EqualFold(allowed, value) {
			return allowed, nil
		}
	}
	return "", fmt.Errorf("допустимые значения: %s", strings.Join(options, ", "))
}
