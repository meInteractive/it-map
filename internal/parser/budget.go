package parser

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

var ErrBudgetUnknown = errors.New("parser budget upper bound unavailable")

type BudgetPeriod struct {
	Day   int64 `json:"day"`
	Month int64 `json:"month"`
}

func SchedulerPeriod(now time.Time) BudgetPeriod {
	msk := time.FixedZone("MSK", 3*60*60)
	local := now.In(msk)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, msk).UTC()
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, msk).UTC()
	return BudgetPeriod{Day: day.UnixMilli(), Month: month.UnixMilli()}
}
func costNumber(v any) (float64, bool) {
	switch value := v.(type) {
	case nil:
		return 0, true
	case float64:
		return value, !math.IsInf(value, 0) && !math.IsNaN(value)
	case bool:
		if value {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.Trim(value, "\u0009\u000A\u000B\u000C\u000D\u0020\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFF")
		if strings.Contains(s, "_") {
			return 0, false
		}
		if s == "" {
			return 0, true
		}
		if len(s) > 2 && (strings.EqualFold(s[:2], "0x") || strings.EqualFold(s[:2], "0b") || strings.EqualFold(s[:2], "0o")) {
			n, err := strconv.ParseUint(s, 0, 64)
			return float64(n), err == nil
		}
		n, err := strconv.ParseFloat(s, 64)
		return n, err == nil && !math.IsInf(n, 0) && !math.IsNaN(n)
	}
	return 0, false
}

// Go's JSON encoder escapes U+2028/U+2029 even with HTML escaping disabled;
// JSON.stringify emits those code points. Count only real string code points,
// never replace textual backslash-u sequences in serialized JSON.
func lineSeparatorCount(v any) int {
	countString := func(s string) int { return strings.Count(s, "\u2028") + strings.Count(s, "\u2029") }
	switch value := v.(type) {
	case string:
		return countString(value)
	case []any:
		n := 0
		for _, item := range value {
			n += lineSeparatorCount(item)
		}
		return n
	case map[string]any:
		n := 0
		for k, item := range value {
			n += countString(k) + lineSeparatorCount(item)
		}
		return n
	}
	return 0
}
func stringifyByteCount(payload map[string]any) (int, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return 0, ErrBudgetUnknown
	}
	return encoded.Len() - 1 - 3*lineSeparatorCount(payload), nil
}
func inputLength(v any) (float64, bool) {
	switch value := v.(type) {
	case []any:
		return float64(len(value)), true
	case string:
		return float64(len(utf16.Encode([]rune(value)))), true
	case map[string]any:
		return costNumber(value["length"])
	default:
		return 0, true
	}
}
func questionCount(v any) int {
	switch value := v.(type) {
	case map[string]any:
		return len(value)
	case []any:
		return len(value)
	case string:
		return len(utf16.Encode([]rune(value)))
	}
	return 0
}

// UpperCostJSON ports legacy arithmetic for JSON provider request metadata.
// It does not reserve money, validate runtime provider configuration or run work.
// Callers still need durable SQL reservation/idempotency and fresh tariffs.
func UpperCostJSON(data []byte) (float64, error) {
	var request map[string]any
	if json.Unmarshal(data, &request) != nil || request == nil {
		return 0, ErrBudgetUnknown
	}
	if free, ok := request["free"].(bool); ok && free {
		return 0, nil
	}
	provider, _ := request["provider"].(string)
	if capUSD, ok := request["capUsd"].(float64); provider == "apify" && ok && capUSD == 0.26 {
		return 0.26, nil
	}
	payload, payloadOK := request["payload"].(map[string]any)
	if !payloadOK {
		return 0, ErrBudgetUnknown
	}
	var upper float64
	if provider == "openai" {
		model, _ := request["model"].(string)
		price, known := historicalPrices[model]
		if !known {
			return 0, ErrBudgetUnknown
		}
		size, err := stringifyByteCount(payload)
		if err != nil {
			return 0, err
		}
		inputs, valid := inputLength(payload["input"])
		if !valid || inputs < 0 || math.IsNaN(inputs) || math.IsInf(inputs, 0) {
			return 0, ErrBudgetUnknown
		}
		output, valid := payload["max_output_tokens"].(float64)
		if !valid || output < 1 || output > maxSafeInteger || math.Trunc(output) != output {
			return 0, ErrBudgetUnknown
		}
		upper = ((float64(size)+4096+inputs*256)*price.Input + output*price.Output) / 1e6
	} else {
		context, valid := costNumber(request["contextLength"])
		pricing, pricingOK := request["pricing"].(map[string]any)
		if !valid || context <= 0 || context > 10000000 || !pricingOK {
			return 0, ErrBudgetUnknown
		}
		prompt, hasPrompt := pricing["prompt"]
		completion, hasCompletion := pricing["completion"]
		p, pOK := costNumber(prompt)
		c, cOK := costNumber(completion)
		fixed, fOK := costNumber(pricing["request"])
		if !hasPrompt || !hasCompletion || !pOK || !cOK || !fOK || p < 0 || c < 0 || fixed < 0 {
			return 0, ErrBudgetUnknown
		}
		upper = (context*(p+c)*2 + fixed) * float64(questionCount(payload["questions"])+1)
	}
	// The legacy expression can overflow despite finite operands. Never reserve
	// an unknown/nonfinite upper bound or allow it to reach a future provider.
	if math.IsNaN(upper) || math.IsInf(upper, 0) || upper < 0 {
		return 0, ErrBudgetUnknown
	}
	return upper, nil
}
