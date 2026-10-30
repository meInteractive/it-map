package parser

import (
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"os"
	"reflect"
	"testing"
)

func checkExtractionRequestSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Name, Raw, Prompt, Title, Company, URL string
		Value                                  json.RawMessage
		Payload                                struct {
			Input           []aiproviders.Message
			Text            struct{ Format json.RawMessage }
			MaxOutputTokens int `json:"max_output_tokens"`
		}
		OK            bool
		Code, Message string
		Status, Calls int
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 7 {
		t.Fatal("source corpus")
	}
	for _, f := range frames {
		t.Run(f.Name, func(t *testing.T) {
			req, e := PrepareExtractionRequest(f.Prompt, f.Raw, ExtractionSource{f.Title, f.Company, &f.URL})
			if f.Calls == 0 {
				var typed *aiproviders.ServiceError
				if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Message != f.Message {
					t.Fatalf("source preflight differs: %v", e)
				}
				return
			}
			if e != nil || req.Task != "vacancy_extraction" || req.MaxTokens != f.Payload.MaxOutputTokens || len(req.Messages) != 2 {
				t.Fatalf("source request unavailable: %v", e)
			}
			msgs := append([]aiproviders.Message(nil), req.Messages...)
			msgs[0].Role = "developer"
			if !reflect.DeepEqual(msgs, f.Payload.Input) {
				t.Fatal("source messages differ")
			}
			var format struct {
				Type string         `json:"type"`
				Spec map[string]any `json:"json_schema"`
			}
			_ = json.Unmarshal(req.ResponseFormat, &format)
			format.Spec["type"] = format.Type
			var expected map[string]any
			_ = json.Unmarshal(f.Payload.Text.Format, &expected)
			if !reflect.DeepEqual(format.Spec, expected) {
				t.Fatal("source JSON schema differs")
			}
			e = req.ValidateContent(string(f.Value))
			if f.OK && e != nil {
				t.Fatal(e)
			}
			if !f.OK {
				var typed *aiproviders.ServiceError
				if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Message != f.Message {
					t.Fatalf("source validation differs: %v", e)
				}
			}
		})
	}
}
func TestExtractionRequestSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/extraction-request-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionRequestSource(t, b)
}
