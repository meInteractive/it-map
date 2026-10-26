package parser

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
	"time"
)

func TestParserBudgetLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/budget-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Prices  map[string]historicalPrice
		Vectors []struct {
			Name  string
			Input json.RawMessage
			Valid bool
			Upper float64
		}
		Periods []struct{ Now, Day, Month int64 }
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) < 70 || len(fixture.Periods) != 10 || len(fixture.Prices) != len(historicalPrices) {
		t.Fatal("required historical budget vectors missing")
	}
	for model, price := range fixture.Prices {
		got, ok := historicalPrices[model]
		if !ok || price != got {
			t.Fatal("historical tariff differs from source fixture")
		}
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := UpperCostJSON(v.Input)
			if !v.Valid {
				if !errors.Is(err, ErrBudgetUnknown) {
					t.Fatal("unknown/overflowing budget was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("valid legacy budget refused")
			}
			if math.Abs(got-v.Upper) > 1e-12*math.Max(1, math.Abs(v.Upper)) {
				t.Fatal("legacy budget upper bound differs")
			}
		})
	}
	for _, v := range fixture.Periods {
		got := SchedulerPeriod(time.UnixMilli(v.Now))
		if got.Day != v.Day || got.Month != v.Month {
			t.Fatal("MSK day/month period differs from legacy")
		}
	}
}
func TestParserBudgetRejectsUnboundedMetadata(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{}`, `{"provider":"openai","model":"gpt-4.1","payload":{"input":{"length":-1000},"max_output_tokens":1}}`, `{"provider":"jev","contextLength":1,"pricing":{"prompt":"Infinity","completion":0},"payload":{}}`, `{"provider":"jev","contextLength":1e999,"pricing":{"prompt":0,"completion":0},"payload":{}}`} {
		if _, err := UpperCostJSON([]byte(data)); !errors.Is(err, ErrBudgetUnknown) {
			t.Fatal("unbounded budget metadata accepted")
		}
	}
}
