package aiproviders

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func checkCatalogSource(t *testing.T, raw []byte) {
	t.Helper()
	var f struct {
		OpenAI []struct {
			ID       string `json:"id"`
			Eligible bool   `json:"eligible"`
			Profile  Model  `json:"profile"`
		} `json:"openai"`
		DefaultModel string `json:"defaultModel"`
		Router       struct {
			Models, Benchmarks, Expected []map[string]any
			At                           string
		}
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.OpenAI) < 60 {
		t.Fatal("source corpus missing")
	}
	models := []Model{}
	for _, x := range f.OpenAI {
		got := ModelProfile(x.ID)
		if TextModel(x.ID) != x.Eligible {
			t.Fatalf("source eligibility differs: %s", x.ID)
		}
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(x.Profile)
		if string(a) != string(b) {
			t.Fatalf("source profile differs: %s", x.ID)
		}
		if x.Eligible {
			models = append(models, got)
		}
	}
	if DefaultModel(models) != f.DefaultModel {
		t.Fatal("source default differs")
	}
	got := RankRouterModels(f.Router.Models, f.Router.Benchmarks, f.Router.At)
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(f.Router.Expected)
	var aa, bb any
	_ = json.Unmarshal(a, &aa)
	_ = json.Unmarshal(b, &bb)
	if !reflect.DeepEqual(aa, bb) {
		t.Fatalf("source OpenRouter ranking differs\n%s\n%s", a, b)
	}
}
func TestModelCatalogSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/model-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkCatalogSource(t, b)
}
func TestOpenAIModelsValidateAndDeduplicate(t *testing.T) {
	if _, e := OpenAIModels([]byte(`{"data":null}`)); e == nil {
		t.Fatal("malformed catalog accepted")
	}
	m, e := OpenAIModels([]byte(`{"data":[{"id":"gpt-5-custom"},{"id":"gpt-6-luna"},{"id":"gpt-5-custom"},{"id":"gpt-5-image"}]}`))
	if e != nil || len(m) != 2 || m[0].ID != "gpt-6-luna" {
		t.Fatal("dedup/filter/priority differs")
	}
}
