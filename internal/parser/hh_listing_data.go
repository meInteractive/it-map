package parser

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"itmap/internal/integrations/apify"
)

var ErrHHListingData = errors.New("неподдерживаемая запись HH")
var ErrHHListingChanged = errors.New("источник или прогресс HH изменились")
var errHHLocation = errors.New("неизвестный город вакансии HH")

// hhMoscowLocation admits only an explicit city, not Moscow Oblast or an
// unqualified remote vacancy. Missing/ambiguous values are quarantined.
func hhMoscowLocation(raw json.RawMessage) (bool, error) {
	var fields map[string]json.RawMessage
	var value string
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(fields["location"], &value) != nil || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false, errHHLocation
	}
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 500 {
		return false, errHHLocation
	}
	city := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ",", 2)[0]))
	city = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(city, "г. "), "г "))
	if city == "москва" || city == "moscow" {
		return true, nil
	}
	if city == "россия" || city == "russia" || city == "удалённо" || city == "удаленно" || city == "remote" || strings.Contains(strings.ToLower(value), "москва") || strings.Contains(strings.ToLower(value), "moscow") {
		return false, errHHLocation
	}
	return false, nil
}

type hhListingVacancy struct{ Title, URL, ExternalID string }

func hhScalar(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number float64
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &number) == nil {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return ""
}
func parseHHListingVacancy(raw json.RawMessage, source string) (hhListingVacancy, error) {
	var fields map[string]json.RawMessage
	var title, link string
	var archived bool
	if json.Unmarshal(raw, &fields) != nil || fields == nil || json.Unmarshal(fields["title"], &title) != nil || json.Unmarshal(fields["url"], &link) != nil || !utf8.ValidString(title) || utf8.RuneCountInString(title) > 500 || strings.ContainsRune(title, 0) || jsStageTrim(title) == "" {
		return hhListingVacancy{}, ErrHHListingData
	}
	_ = json.Unmarshal(fields["archived"], &archived)
	if archived {
		return hhListingVacancy{}, ErrHHListingData
	}
	input, e := apify.HHInput(link, apify.HHOptions{Detail: true})
	if e != nil {
		return hhListingVacancy{}, ErrHHListingData
	}
	parsed, _ := url.Parse(input.StartURL)
	id := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/vacancy/"), "/")
	if rawID := fields["vacancy_id"]; len(rawID) > 0 && string(rawID) != "null" && hhScalar(rawID) != id {
		return hhListingVacancy{}, ErrHHListingData
	}
	sourceInput, e := apify.HHInput(source, apify.HHOptions{})
	if e != nil {
		return hhListingVacancy{}, ErrHHListingData
	}
	scope, _ := url.Parse(sourceInput.StartURL)
	if employer := scope.Query().Get("employer_id"); employer != "" && hhScalar(fields["employer_id"]) != employer {
		return hhListingVacancy{}, ErrHHListingData
	}
	if strings.HasPrefix(scope.Path, "/vacancy/") && strings.TrimSuffix(strings.TrimPrefix(scope.Path, "/vacancy/"), "/") != id {
		return hhListingVacancy{}, ErrHHListingData
	}
	return hhListingVacancy{jsStageTrim(title), "https://hh.ru/vacancy/" + id, id}, nil
}

var hhTitleTerms = map[string][]string{
	"root-it-manager": {"руководител*", "директор*", "лид*", "менеджер*", "начальник*", "управля*", "владел*", "продукт*", "проект*", "продакт*", "проджект*", "тимлид*", "teamlead*", "head", "lead*", "manager", "director", "owner", "product", "project", "program", "delivery", "chief", "CTO", "CPO", "CIO"},
	"root-journalist": {"журналист*", "редактор*", "корреспондент*", "обозревател*", "спецкор*", "автор*", "editor", "journalist", "reporter", "writer", "columnist", "correspondent"},
}

// Неизвестная активная роль снимает предварительное сужение: семантическое
// решение остаётся у классификации, фильтр HH не должен терять новые роли.
func hhTitleQuery(keys []string) *string {
	if len(keys) == 0 {
		return nil
	}
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	seenKeys := map[string]bool{}
	seenTerms := map[string]bool{}
	terms := []string{}
	for _, key := range keys {
		words, ok := hhTitleTerms[key]
		if !ok {
			return nil
		}
		if seenKeys[key] {
			continue
		}
		seenKeys[key] = true
		for _, word := range words {
			if !seenTerms[word] {
				terms = append(terms, word)
				seenTerms[word] = true
			}
		}
	}
	query := "NAME:(" + strings.Join(terms, " OR ") + ")"
	return &query
}
