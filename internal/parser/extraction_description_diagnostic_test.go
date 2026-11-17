package parser

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"itmap/internal/aiproviders"
)

func TestDescriptionFailureCategoryPreservesPublicError(t *testing.T) {
	var schema struct{ Required []string }
	if json.Unmarshal(ExtractionSchema(), &schema) != nil {
		t.Fatal("invalid synthetic schema")
	}
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"null", nil, "wrong_type"}, {"number", float64(7), "wrong_type"}, {"empty", "  ", "empty"}, {"nul", "CI_PRIVATE\x00CONTENT", "null_byte"}, {"long", strings.Repeat("x", 150001), "too_long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{}
			for _, key := range schema.Required {
				payload[key] = nil
			}
			payload["title"] = "CI_TITLE"
			payload["level"] = "Senior"
			payload["description_md"] = tc.value
			raw, _ := json.Marshal(payload)
			_, err := NormalizeExtraction(string(raw), "CI_SOURCE")
			var service *aiproviders.ServiceError
			if !errors.As(err, &service) || service.Code != "invalid_output" || service.Status != 422 || service.Message != "Некорректное поле description_md." {
				t.Fatal("public validation changed", err)
			}
			var stored map[string]any
			if json.Unmarshal(extractionFailureDiagnostic(err), &stored) != nil || stored["descriptionIssue"] != tc.want || stored["costEstimated"] != false {
				t.Fatal("wrong diagnostic", stored)
			}
			if strings.Contains(string(extractionFailureDiagnostic(err)), "CI_PRIVATE") || strings.Contains(string(extractionFailureDiagnostic(err)), "CI_SOURCE") {
				t.Fatal("private data leaked")
			}
		})
	}
}
func TestDescriptionDiagnosticRejectsUntrustedErrorCategory(t *testing.T) {
	err := &descriptionValidationError{cause: &aiproviders.ServiceError{Code: "invalid_output", Status: 422, Message: "Некорректное поле description_md."}, reason: "CI_PRIVATE"}
	if strings.Contains(string(extractionFailureDiagnostic(err)), "CI_PRIVATE") {
		t.Fatal("untrusted category leaked")
	}
	raw := string(extractionFailureDiagnostic(errors.New("CI_PRIVATE")))
	if strings.Contains(raw, "CI_PRIVATE") || strings.Contains(raw, "descriptionIssue") {
		t.Fatal("provider error leaked", raw)
	}
}
