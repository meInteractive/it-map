package parser

import (
	"encoding/json"
	"itmap/internal/aiproviders"
	"itmap/internal/backup"
)

// Источник передаётся модели как данные, а не как инструкции.
type ExtractionSource struct {
	Title   string  `json:"title"`
	Company string  `json:"company"`
	URL     *string `json:"url"`
}

func PrepareExtractionRequest(prompt, markdown string, source ExtractionSource) (aiproviders.GenerationRequest, error) {
	if utf16Length(markdown) > ExtractionInputLimit {
		return aiproviders.GenerationRequest{}, &aiproviders.ServiceError{Code: "input_too_large", Status: 422, Message: "Исходный Markdown сохранён целиком, но превышает безопасный лимит 120 000 символов для запроса LLM. Структурирование ожидает обработки без усечения."}
	}
	if jsStageTrim(prompt) == "" {
		return aiproviders.GenerationRequest{}, &aiproviders.ServiceError{Code: "empty_prompt", Status: 409, Message: "Заполните промт «Структурирование вакансии»."}
	}
	schema, e := backup.StringifyParsedJSON(ExtractionSchema())
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrSettingsStorage
	}
	body, e := json.Marshal(struct {
		Source   ExtractionSource `json:"source"`
		Markdown string           `json:"markdown"`
	}{source, markdown})
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	body, e = backup.StringifyParsedJSON(body)
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	format, e := json.Marshal(struct {
		Type string `json:"type"`
		Spec struct {
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"json_schema"`
	}{Type: "json_schema", Spec: struct {
		Name   string          `json:"name"`
		Strict bool            `json:"strict"`
		Schema json.RawMessage `json:"schema"`
	}{"vacancy_details", true, schema}})
	if e != nil {
		return aiproviders.GenerationRequest{}, ErrJobInput
	}
	return aiproviders.GenerationRequest{Task: "vacancy_extraction", MaxTokens: 32768, ResponseFormat: format, Messages: []aiproviders.Message{{Role: "system", Content: prompt + "\nСерверная схема:\n" + string(schema)}, {Role: "user", Content: string(body)}}, ValidateContent: func(content string) error { _, e := NormalizeExtraction(content, markdown); return e }}, nil
}
