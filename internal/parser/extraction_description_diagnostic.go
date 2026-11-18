package parser

import (
	"encoding/json"
	"errors"
	"strings"

	"itmap/internal/aiproviders"
)

// descriptionValidationError wraps the existing public error, never the model
// response or vacancy text. The reason is a fixed, non-content category.
type descriptionValidationError struct {
	cause  error
	reason string
}

func (e *descriptionValidationError) Error() string { return e.cause.Error() }
func (e *descriptionValidationError) Unwrap() error { return e.cause }

func descriptionInvalidReason(value any) string {
	text, ok := value.(string)
	if !ok {
		return "wrong_type"
	}
	if jsStageTrim(text) == "" {
		return "empty"
	}
	if strings.ContainsRune(text, 0) {
		return "null_byte"
	}
	if utf16Length(text) > 150000 {
		return "too_long"
	}
	return "other"
}

// extractionOutputIssue классифицирует только фиксированные сообщения,
// которые генерируют наши валидаторы и адаптеры. Данные ответа провайдера,
// имена вакансий и произвольные тексты ошибки не копируются в диагностику.
func extractionOutputIssue(failure error) string {
	var service *aiproviders.ServiceError
	if !errors.As(failure, &service) || service.Code != "invalid_output" {
		return ""
	}
	if service.Status == 502 {
		switch service.Message {
		case "OpenAI не вернул полный ответ в нужном формате. Результат не сохранён.",
			"Модель не вернула полный структурированный ответ. Markdown сохранён.":
			// Существующий контракт не различает пустой контент, refusal,
			// неверный envelope и незавершённый ответ: не выдумываем точную причину.
			return "provider_response"
		}
	}
	if service.Status != 422 {
		return ""
	}
	switch service.Message {
	case "Модель вернула некорректный JSON.":
		return "json_invalid"
	case "Поля ответа не соответствуют схеме.":
		return "schema_shape"
	case "Недостаточное содержательное описание.":
		return "content_insufficient"
	case "Границы зарплаты перепутаны.":
		return "salary_bounds"
	case "Зарплата не подтверждается исходным текстом.":
		return "salary_unverified"
	case "Дата публикации должна быть действительной датой YYYY-MM-DD.":
		return "publication_date"
	case "Значение work_format не подтверждается исходным текстом.",
		"Значение salary_currency не подтверждается исходным текстом.",
		"Значение salary_tax_type не подтверждается исходным текстом.",
		"Значение salary_period не подтверждается исходным текстом.",
		"Валюта не подтверждена.",
		"Дата публикации не подтверждается источником.":
		// Ошибка проверки происхождения извлечённого значения — не ошибка
		// структуры JSON. Не включать в receipt нераспознанный текст ошибки
		// или данные источника: только фиксированная безопасная категория.
		return "source_unverified"
	}
	var schema struct{ Required []string }
	if json.Unmarshal(ExtractionSchema(), &schema) == nil {
		for _, field := range schema.Required {
			if service.Message == "Некорректное поле "+field+"." {
				return "field_invalid"
			}
		}
	}
	return ""
}

// extractionFailureDiagnostic принимает только ограниченные категории из
// локального валидатора. Произвольный текст провайдера не сохраняется.
func extractionFailureDiagnostic(failure error) []byte {
	diagnostic := map[string]any{"message": extractionFailureMessage(failure), "costEstimated": false}
	if issue := extractionOutputIssue(failure); issue != "" {
		diagnostic["outputIssue"] = issue
	}
	var issue *descriptionValidationError
	var service *aiproviders.ServiceError
	if errors.As(failure, &issue) && errors.As(failure, &service) && service.Code == "invalid_output" && service.Status == 422 && service.Message == "Некорректное поле description_md." {
		switch issue.reason {
		case "wrong_type", "empty", "null_byte", "too_long":
			diagnostic["descriptionIssue"] = issue.reason
		}
	}
	raw, _ := json.Marshal(diagnostic)
	return raw
}
