package parser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
		{"source_evidence", "invalid_output", "Значение work_format не подтверждается исходным текстом.", "source_unverified", 422},
		{"source_evidence_currency", "invalid_output", "Значение salary_currency не подтверждается исходным текстом.", "source_unverified", 422},
		{"source_evidence_tax", "invalid_output", "Значение salary_tax_type не подтверждается исходным текстом.", "source_unverified", 422},
		{"source_evidence_period", "invalid_output", "Значение salary_period не подтверждается исходным текстом.", "source_unverified", 422},
		{"source_evidence_currency_fallback", "invalid_output", "Валюта не подтверждена.", "source_unverified", 422},
		{"source_evidence_date", "invalid_output", "Дата публикации не подтверждается источником.", "source_unverified", 422},
		{"source_evidence_unknown", "invalid_output", "Значение CI_PRIVATE_FIELD не подтверждается исходным текстом.", "", 422},
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

// GO-431: проверяем реальный валидатор, а не только подставленные сообщения.
// Контрактные фикстуры используют вымышленные значения; запросов AI здесь нет.
func TestGO431ExtractionIssuesFromValidationCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/extraction-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Frames []struct {
			Name, Content, Raw string
			Strict, OK         bool
			Code, Message      string
			Status             int
		}
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"Модель вернула некорректный JSON.": "json_invalid",
		"Поля ответа не соответствуют схеме.": "schema_shape",
		"Недостаточное содержательное описание.": "content_insufficient",
		"Границы зарплаты перепутаны.": "salary_bounds",
		"Зарплата не подтверждается исходным текстом.": "salary_unverified",
		"Дата публикации должна быть действительной датой YYYY-MM-DD.": "publication_date",
		"Значение work_format не подтверждается исходным текстом.": "source_unverified",
	}
	seen, sourceEvidence := 0, 0
	for _, frame := range corpus.Frames {
		if frame.OK {
			continue
		}
		t.Run(frame.Name, func(t *testing.T) {
			_, validationErr := normalizeExtraction(frame.Content, frame.Raw, frame.Strict)
			var service *aiproviders.ServiceError
			if !errors.As(validationErr, &service) || service.Code != frame.Code ||
				service.Status != frame.Status || service.Message != frame.Message {
				t.Fatal("synthetic source validator changed its failure contract")
			}
			want := expected[frame.Message]
			if strings.HasPrefix(frame.Message, "Некорректное поле ") && strings.HasSuffix(frame.Message, ".") {
				want = "field_invalid"
			}
			if want == "" {
				t.Fatal("new validation failure requires deliberate bounded mapping", frame.Name)
			}
			var diagnostic map[string]any
			if err := json.Unmarshal(extractionFailureDiagnostic(validationErr), &diagnostic); err != nil {
				t.Fatal(err)
			}
			if diagnostic["outputIssue"] != want || diagnostic["costEstimated"] != false {
				t.Fatal("failure lost its safe category or changed unknown-cost semantics", frame.Name, diagnostic)
			}
			for _, sensitive := range []string{frame.Content, frame.Raw, "CI_PRIVATE"} {
				if len(sensitive) >= 3 && strings.Contains(string(extractionFailureDiagnostic(validationErr)), sensitive) {
					t.Fatal("raw source or model response disclosed", frame.Name)
				}
			}
			seen++
			if want == "source_unverified" {
				sourceEvidence++
			}
		})
	}
	if seen < 20 || sourceEvidence < 1 {
		t.Fatalf("insufficient source-oracle coverage: failed=%d evidence=%d", seen, sourceEvidence)
	}
}

// Ошибки транспорта/отмены и неразрешённый платный исход — не
// invalid_output, даже если в исходной ошибке присутствует приватный ответ.
func TestGO431ExtractionFailureNeverInventsKnownCostOrOutputIssue(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"cancelled", context.Canceled, "cancelled"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"uncertain_paid", ErrMoneyUncertain, "money_uncertain"},
		{"untrusted_provider_message", &aiproviders.ServiceError{Code: "invalid_output", Status: 502, Message: "CI_PRIVATE_UNTRUSTED_PROVIDER_BODY"}, "invalid_output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := aiStageErrorCode(tc.err); got != tc.code {
				t.Fatalf("public error classification changed: %s, want %s", got, tc.code)
			}
			raw := extractionFailureDiagnostic(tc.err)
			var diagnostic map[string]any
			if json.Unmarshal(raw, &diagnostic) != nil {
				t.Fatal("invalid diagnostic")
			}
			if _, known := diagnostic["outputIssue"]; known {
				t.Fatal("unknown response mislabelled as known validation reason")
			}
			if diagnostic["costEstimated"] != false || diagnostic["message"] != "Запрос прерван; результат и стоимость неизвестны." {
				t.Fatal("unknown paid outcome was treated as settled or valid", diagnostic)
			}
			if strings.Contains(string(raw), "CI_PRIVATE") || strings.Contains(string(raw), "costUsd") {
				t.Fatal("raw response or invented cost leaked")
			}
		})
	}
}
