package parser

import (
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"os"
	"reflect"
	"testing"
)

func checkExtractionSource(t *testing.T, raw []byte) {
	t.Helper()
	var corpus struct {
		Schema         json.RawMessage
		Version, Limit int
		Frames         []struct {
			Name, Content, Raw string
			Strict, OK         bool
			Result             json.RawMessage
			Code, Message      string
			Status             int
		}
	}
	if json.Unmarshal(raw, &corpus) != nil || len(corpus.Frames) < 35 || corpus.Version != ExtractionSchemaVersion || corpus.Limit != ExtractionInputLimit {
		t.Fatal("source schema corpus")
	}
	var a, b any
	_ = json.Unmarshal(corpus.Schema, &a)
	_ = json.Unmarshal(ExtractionSchema(), &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("source schema differs")
	}
	for _, f := range corpus.Frames {
		t.Run(f.Name, func(t *testing.T) {
			got, e := normalizeExtraction(f.Content, f.Raw, f.Strict)
			if !f.OK {
				var v *aiproviders.ServiceError
				if !errors.As(e, &v) || v.Code != f.Code || v.Status != f.Status || v.Message != f.Message {
					t.Fatalf("source error differs %v want %s", e, f.Message)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(got)
			var actual, want any
			_ = json.Unmarshal(raw, &actual)
			_ = json.Unmarshal(f.Result, &want)
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("source result differs %s want %s", raw, f.Result)
			}
		})
	}
}
func TestExtractionSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/extraction-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkExtractionSource(t, b)
}
