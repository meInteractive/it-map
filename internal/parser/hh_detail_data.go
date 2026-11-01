package parser

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"itmap/internal/integrations/hhpublic"
)

var ErrHHDetailInvalid = errors.New("HH не вернул проверенное полное описание вакансии")
var ErrHHDetailPending = errors.New("Apify ещё получает полный текст HH")

type hhDetailDocument struct{ Title, URL, Description, Markdown, Format string }

func hhJoinedField(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return ""
	}
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = hhScalar(value)
		if string(value) == "true" || string(value) == "false" {
			parts[i] = string(value)
		}
	}
	return strings.Join(parts, ", ")
}

// Проверка применяется к самому описанию: метаданные не могут превратить
// короткий ответ или CAPTCHA в содержательный снимок для модели.
func parseHHDetail(raw json.RawMessage, target string) (hhDetailDocument, error) {
	if !utf8.Valid(raw) || len(raw) > 2200000 {
		return hhDetailDocument{}, ErrHHDetailInvalid
	}
	vacancy, err := parseHHListingVacancy(raw, target)
	if err != nil {
		return hhDetailDocument{}, ErrHHDetailInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return hhDetailDocument{}, ErrHHDetailInvalid
	}
	var description string
	_ = json.Unmarshal(fields["description_text"], &description)
	format := "text"
	if jsStageTrim(description) == "" {
		format = "html"
		var html string
		_ = json.Unmarshal(fields["description_html"], &html)
		description = hhpublic.HTMLText(html)
	}
	if len(CheckFulltextContent(description, 200)) != 0 {
		return hhDetailDocument{}, ErrHHDetailInvalid
	}
	metadata := []string{}
	for _, field := range [][2]string{{"Компания", "company"}, {"Местоположение", "location"}, {"Зарплата", "salary"}, {"Опыт", "experience"}, {"Занятость", "employment_type"}, {"График", "schedule"}, {"Формат работы", "work_format"}, {"Навыки", "skills"}, {"Дата публикации", "date_posted"}} {
		var value string
		if field[1] == "work_format" || field[1] == "skills" {
			value = hhJoinedField(fields[field[1]])
		} else {
			_ = json.Unmarshal(fields[field[1]], &value)
		}
		if jsStageTrim(value) != "" {
			metadata = append(metadata, field[0]+": "+value)
		}
	}
	return hhDetailDocument{vacancy.Title, vacancy.URL, description, "# " + vacancy.Title + "\n\n" + strings.Join(metadata, "\n\n") + "\n\n" + description, format}, nil
}
