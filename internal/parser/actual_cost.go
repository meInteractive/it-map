package parser

import "encoding/json"

func jsonNumber(raw json.RawMessage) (float64, bool) {
	var n float64
	if len(raw) == 0 || string(raw) == "null" || raw[0] == '"' || raw[0] == 't' || raw[0] == 'f' || json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, finiteMoney(n)
}

// Unchanged schedulerBilling precedence: finite explicit usage.cost, known
// free request, then historical OpenAI token tariff only without cached input.
// Missing usage never releases a paid reservation. Unrelated malformed fields
// cannot override an explicit cost or a known free request.
func actualCost(request, response json.RawMessage) (float64, bool) {
	var req, res, usage map[string]json.RawMessage
	if json.Unmarshal(request, &req) != nil || json.Unmarshal(response, &res) != nil {
		return 0, false
	}
	_ = json.Unmarshal(res["usage"], &usage)
	if cost, known := jsonNumber(usage["cost"]); known {
		return cost, true
	}
	var free bool
	_ = json.Unmarshal(req["free"], &free)
	if free {
		return 0, true
	}
	var provider, model string
	_ = json.Unmarshal(req["provider"], &provider)
	_ = json.Unmarshal(req["model"], &model)
	if provider != "openai" {
		return 0, false
	}
	var responseModel any
	if raw := res["model"]; len(raw) > 0 {
		if json.Unmarshal(raw, &responseModel) != nil {
			return 0, false
		}
		if truthy(responseModel) {
			var ok bool
			model, ok = responseModel.(string)
			if !ok {
				return 0, false
			}
		}
	}
	price, known := historicalPrices[model]
	if !known {
		return 0, false
	}
	input, inOK := jsonNumber(usage["input_tokens"])
	output, outOK := jsonNumber(usage["output_tokens"])
	if !inOK || !outOK {
		return 0, false
	}
	var details map[string]json.RawMessage
	_ = json.Unmarshal(usage["input_tokens_details"], &details)
	if raw := details["cached_tokens"]; len(raw) > 0 {
		var cached any
		if json.Unmarshal(raw, &cached) != nil || truthy(cached) {
			return 0, false
		}
	}
	cost := (input*price.Input + output*price.Output) / 1e6
	return cost, finiteMoney(cost)
}
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	default:
		return true
	}
}
