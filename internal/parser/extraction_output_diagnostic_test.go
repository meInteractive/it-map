package parser

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"itmap/internal/aiproviders"
)

func TestExtractionOutputIssueSafeCategories(t *testing.T) {
	for _, tc := range []struct {
		name, code, message, category string
		status                        int
	}{
		{"malformed_json", "invalid_output", "Модель вернула некорректный JSON.", "json_invalid", 422},
		{"schema_shape", "invalid_output", "Поля ответа не соответствуют схеме.", "schema_shape", 422},
		{"missing_content", "invalid_output", "Недостаточное содержательное описание.", "content_insufficient", 422},
		{"salary_bounds", "invalid_output", "Границы зарплаты перепутаны.", "salary_bounds", 422},
		{"salary_evidence", "invalid_output", "Зарплата не подтверждается исходным текстом.", "salary_unverified", 422},
		{"publication_date", "invalid_output", "Дата публикации должна быть действительной датой YYYY-MM-DD.", "publication_date", 422},
		{"known_description_field", "invalid_output", "Некорректное поле description_md.", "field_invalid", 422},
		{"known_title_field", "invalid_output", "Некорректное поле title.", "field_invalid", 422},
		{"openai_envelope", "invalid_output", "OpenAI не вернул полный ответ в нужном формате. Результат не сохранён.", "provider_response", 502},
		{"router_envelope", "invalid_output", "Модель не вернула полный структурированный ответ. Markdown сохранён.", "provider_response", 502},
		{"unknown_422", "invalid_output", "CI_PRIVATE_PROVIDER_RESPONSE", "", 422},
		{"unknown_502", "invalid_output", "CI_PRIVATE_PROVIDER_RESPONSE", "", 502},
		{"untrusted_field", "invalid_output", "Некорректное поле CI_PRIVATE_FIELD.", "", 422},
		{"wrong_status", "invalid_output", "Модель вернула некорректный JSON.", "", 503},
		{"wrong_code", "unavailable", "Модель вернула некорректный JSON.", "", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := &aiproviders.ServiceError{Code: tc.code, Status: tc.status, Message: tc.message}
			var data map[string]any
			raw := extractionFailureDiagnostic(e)
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal("diagnostic is not valid JSON", err)
			}
			value, exists := data["outputIssue"]
			if tc.category == "" {
				if exists {
					t.Fatal("untrusted error became a diagnostic category", value)
				}
			} else if !exists || value != tc.category {
				t.Fatal("wrong bounded diagnostic category", value, tc.category)
			}
			if strings.Contains(string(raw), "CI_PRIVATE") || data["costEstimated"] != false {
				t.Fatal("diagnostic disclosed untrusted content or invented spend")
			}
			var unwrapped *aiproviders.ServiceError
			if !errors.As(e, &unwrapped) || unwrapped.Code != tc.code {
				t.Fatal("existing public error category changed")
			}
		})
	}
}

func TestExtractionOutputIssueFromSyntheticValidation(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"json_invalid", "CI_PRIVATE_NOT_JSON", "json_invalid"},
		{"schema_shape", "{}", "schema_shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NormalizeExtraction(tc.raw, "CI_PRIVATE_VACANCY_SOURCE")
			var service *aiproviders.ServiceError
			if !errors.As(err, &service) || service.Code != "invalid_output" || service.Status != 422 {
				t.Fatal("validation contract changed", err)
			}
			raw := extractionFailureDiagnostic(err)
			var out map[string]any
			if json.Unmarshal(raw, &out) != nil || out["outputIssue"] != tc.want || strings.Contains(string(raw), "CI_PRIVATE") {
				t.Fatal("validator output category invalid or unsafe", out)
			}
		})
	}
}

func TestExtractionOutputIssueDoesNotLeakWrappedError(t *testing.T) {
	e := errors.Join(errors.New("CI_PRIVATE_JOB_PAYLOAD"), &aiproviders.ServiceError{Code: "invalid_output", Status: 422, Message: "Поля ответа не соответствуют схеме."})
	raw := extractionFailureDiagnostic(e)
	if strings.Contains(string(raw), "CI_PRIVATE") || !strings.Contains(string(raw), `"outputIssue":"schema_shape"`) {
		t.Fatal("wrapped provider error contaminated diagnostic")
	}
}
