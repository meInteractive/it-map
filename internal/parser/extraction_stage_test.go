package parser

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func checkExtractionStageSource(t *testing.T, raw []byte) {
	t.Helper()
	var corpus struct {
		URLFrames []struct {
			URL string
			OK  bool
		}
		UsageFrames []struct {
			Provider, Model string
			Usage           json.RawMessage
			Result          json.RawMessage
		}
	}
	if json.Unmarshal(raw, &corpus) != nil || len(corpus.URLFrames) < 15 || len(corpus.UsageFrames) < 8 {
		t.Fatal("source corpus")
	}
	for _, f := range corpus.URLFrames {
		t.Run("url", func(t *testing.T) {
			if validExtractionURL(f.URL) != f.OK {
				t.Fatal("source URL safety differs", f.URL)
			}
		})
	}
	for _, f := range corpus.UsageFrames {
		t.Run("usage", func(t *testing.T) {
			input, output, cost, estimated := extractionUsage(f.Usage, f.Provider, f.Model)
			actual, _ := json.Marshal(map[string]any{"input": input, "output": output, "cost": cost, "estimated": estimated})
			var got, want any
			_ = json.Unmarshal(actual, &got)
			_ = json.Unmarshal(f.Result, &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("source cost differs: %s %s", actual, f.Result)
			}
		})
	}
}
func TestExtractionStageSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/extraction-stage-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionStageSource(t, b)
}
func TestExtractionUnknownCachedTokensNeverEstimateCost(t *testing.T) {
	_, _, cost, estimated := extractionUsage([]byte(`{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":"unknown"}}`), "openai", "gpt-6-luna")
	if cost != nil || estimated {
		t.Fatal("malformed cache tokens estimated as actual cost")
	}
}
