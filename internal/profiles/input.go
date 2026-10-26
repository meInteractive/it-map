// Package profiles preserves personal resume and shared prompt/model contracts.
package profiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

const MaxResumeLength = 20000
const MaxSafeRevision int64 = 9007199254740991

type InputError struct {
	Status  int
	Message string
}

func (e *InputError) Error() string            { return e.Message }
func invalid(status int, message string) error { return &InputError{Status: status, Message: message} }
func ErrorStatus(err error) int {
	if err == nil {
		return 200
	}
	var input *InputError
	if errors.As(err, &input) {
		return input.Status
	}
	return 503
}

type TextInput struct {
	Text     string
	Revision int64
}
type ModelInput struct {
	Provider        string `json:"provider"`
	OpenAIModel     string `json:"openaiModel"`
	OpenRouterModel string `json:"openrouterModel"`
	ReasoningEffort string `json:"reasoningEffort"`
	Revision        int64  `json:"revision"`
}

func KnownKind(kind string) bool {
	switch kind {
	case "cover_letter", "relevance", "vacancy_extraction", "vacancy_role":
		return true
	}
	return false
}
func PromptLimit(kind string) int {
	switch kind {
	case "cover_letter":
		return 15000
	case "relevance", "vacancy_role":
		return 5000
	case "vacancy_extraction":
		return 10000
	}
	return 0
}
func PromptBodyLimit(kind string) int {
	if PromptLimit(kind) >= 10000 {
		return 128 << 10
	}
	return 32 << 10
}
func decodeObject(body []byte, declaredLength int64, limit int, allowed []string, message string) (map[string]json.RawMessage, error) {
	if declaredLength > int64(limit) || len(body) > limit {
		return nil, invalid(413, "Запрос слишком большой.")
	}
	// TextDecoder in the legacy streamed readers consumes one UTF-8 BOM.
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, invalid(400, "Некорректные данные.")
	}
	if object == nil {
		return nil, invalid(400, message)
	}
	for key := range object {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return nil, invalid(400, message)
		}
	}
	// JSON.parse keeps the last duplicate property; the map decoder does too.
	return object, nil
}
func textField(object map[string]json.RawMessage, key string) (string, bool) {
	var value *string
	raw, ok := object[key]
	if !ok || json.Unmarshal(raw, &value) != nil || value == nil {
		return "", false
	}
	return *value, true
}
func revisionField(object map[string]json.RawMessage) (int64, bool) {
	raw, ok := object["revision"]
	if !ok {
		return 0, false
	}
	var number *float64
	if json.Unmarshal(raw, &number) != nil || number == nil || math.IsInf(*number, 0) || math.IsNaN(*number) || *number < 0 || *number > float64(MaxSafeRevision) || math.Trunc(*number) != *number {
		return 0, false
	}
	return int64(*number), true
}
func ValidateResume(input TextInput) error {
	if !utf8.ValidString(input.Text) || strings.ContainsRune(input.Text, 0) || utf8.RuneCountInString(input.Text) > MaxResumeLength {
		return invalid(400, "Резюме должно содержать не более 20 000 символов без нулевого символа.")
	}
	if input.Revision < 0 || input.Revision > MaxSafeRevision {
		return invalid(400, "Сначала загрузите текущую версию резюме.")
	}
	return nil
}
func ValidatePrompt(kind string, input TextInput) error {
	limit := PromptLimit(kind)
	if limit == 0 {
		return invalid(404, "Промт не найден.")
	}
	if !utf8.ValidString(input.Text) || strings.ContainsRune(input.Text, 0) || utf8.RuneCountInString(input.Text) > limit {
		count := map[int]string{5000: "5\u00a0000", 10000: "10\u00a0000", 15000: "15\u00a0000"}[limit]
		return invalid(400, "Промт должен содержать не более "+count+" символов без нулевого символа.")
	}
	if input.Revision < 0 || input.Revision > MaxSafeRevision {
		return invalid(400, "Сначала загрузите текущую версию промта.")
	}
	return nil
}
func ParseResume(body []byte, declaredLength int64) (TextInput, error) {
	return parseText("", body, declaredLength)
}
func ParsePrompt(kind string, body []byte, declaredLength int64) (TextInput, error) {
	if !KnownKind(kind) {
		return TextInput{}, invalid(404, "Промт не найден.")
	}
	return parseText(kind, body, declaredLength)
}
func parseText(kind string, body []byte, declaredLength int64) (TextInput, error) {
	field, limit, message := "resumeMd", 256<<10, "Передайте resumeMd и revision своего резюме."
	if kind != "" {
		field, limit, message = "markdown", PromptBodyLimit(kind), "Передайте markdown и revision промта."
	}
	object, err := decodeObject(body, declaredLength, limit, []string{field, "revision"}, message)
	if err != nil {
		return TextInput{}, err
	}
	text, ok := textField(object, field)
	if !ok {
		return TextInput{}, invalid(400, message)
	}
	// Match legacy validation order: invalid text precedes invalid revision.
	input := TextInput{Text: text}
	if kind == "" {
		err = ValidateResume(input)
	} else {
		err = ValidatePrompt(kind, input)
	}
	if err != nil {
		return TextInput{}, err
	}
	revision, ok := revisionField(object)
	if !ok {
		if kind == "" {
			return TextInput{}, invalid(400, "Сначала загрузите текущую версию резюме.")
		}
		return TextInput{}, invalid(400, "Сначала загрузите текущую версию промта.")
	}
	input.Revision = revision
	return input, nil
}

var modelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,159}$`)

func ValidateModel(kind string, input ModelInput) error {
	if !KnownKind(kind) {
		return invalid(404, "Промт не найден.")
	}
	if input.Provider != "openrouter" && input.Provider != "openai" || input.OpenAIModel != "" && !modelID.MatchString(input.OpenAIModel) || input.Revision < 0 || input.Revision > MaxSafeRevision {
		return invalid(400, "Некорректные настройки модели.")
	}
	effort := false
	for _, allowed := range []string{"auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if input.ReasoningEffort == allowed {
			effort = true
			break
		}
	}
	if !effort {
		return invalid(400, "Некорректные настройки модели.")
	}
	if input.OpenRouterModel != "best_free" && input.OpenRouterModel != "jev" {
		return invalid(400, "Некорректная модель OpenRouter.")
	}
	if input.OpenRouterModel == "jev" && kind != "relevance" && kind != "vacancy_role" {
		return invalid(400, "JEV доступна для релевантности и определения конечных ролей.")
	}
	return nil
}

// ParseModel validates the stored shape and kind restriction only. Live model
// availability/capabilities and JEV catalog checks belong to the provider gate.
func ParseModel(kind string, body []byte, declaredLength int64) (ModelInput, error) {
	if !KnownKind(kind) {
		return ModelInput{}, invalid(404, "Промт не найден.")
	}
	message := "Некорректные настройки модели."
	object, err := decodeObject(body, declaredLength, 4096, []string{"provider", "openaiModel", "openrouterModel", "reasoningEffort", "revision"}, message)
	if err != nil {
		return ModelInput{}, err
	}
	provider, p := textField(object, "provider")
	model, m := textField(object, "openaiModel")
	effort, e := textField(object, "reasoningEffort")
	revision, r := revisionField(object)
	if !p || !m || !e || !r {
		return ModelInput{}, invalid(400, message)
	}
	router := "best_free"
	if _, present := object["openrouterModel"]; present {
		var ok bool
		router, ok = textField(object, "openrouterModel")
		if !ok {
			return ModelInput{}, invalid(400, "Некорректная модель OpenRouter.")
		}
	}
	input := ModelInput{Provider: provider, OpenAIModel: model, OpenRouterModel: router, ReasoningEffort: effort, Revision: revision}
	return input, ValidateModel(kind, input)
}
