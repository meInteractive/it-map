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

// extractionFailureDiagnostic accepts only a bounded category created by the
// local validator; arbitrary provider messages and raw input are never stored.
func extractionFailureDiagnostic(failure error) []byte {
	diagnostic := map[string]any{"message": extractionFailureMessage(failure), "costEstimated": false}
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
