package profiles

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type inputVector struct {
	Type, Kind, Name string
	Input            map[string]any
	TextSpec         *struct {
		Field, Unit, Suffix string
		Count               int
	}
	Raw               *string
	PrefixBOM, NoBody bool
	BodyRepeat        int
	DeclaredLength    int64
	Status            int
	Normalized        map[string]any
}

func (v inputVector) body(t *testing.T) []byte {
	t.Helper()
	if v.NoBody {
		return nil
	}
	if v.BodyRepeat > 0 {
		return []byte(strings.Repeat(" ", v.BodyRepeat))
	}
	if v.Raw != nil {
		return []byte(*v.Raw)
	}
	input := map[string]any{}
	for k, value := range v.Input {
		input[k] = value
	}
	if v.TextSpec != nil {
		s := v.TextSpec
		input[s.Field] = strings.Repeat(s.Unit, s.Count) + s.Suffix
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if v.PrefixBOM {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	return data
}
func loadContract(t *testing.T) struct {
	Vectors  []inputVector
	Mappings []struct {
		Kind          string
		Prompt, Model Binding
	}
	Permissions []struct {
		Surface, Kind, Method, User, Owner string
		Admin, Storage                     bool
		Status                             int
	}
} {
	t.Helper()
	var result struct {
		Vectors  []inputVector
		Mappings []struct {
			Kind          string
			Prompt, Model Binding
		}
		Permissions []struct {
			Surface, Kind, Method, User, Owner string
			Admin, Storage                     bool
			Status                             int
		}
	}
	data, err := os.ReadFile("testdata/input-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestLegacyProfileInputContracts(t *testing.T) {
	fixture := loadContract(t)
	if len(fixture.Vectors) != 278 {
		t.Fatal("source-backed profile input vectors missing")
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Type+"/"+v.Kind+"/"+v.Name, func(t *testing.T) {
			var err error
			var normalized map[string]any
			if v.Type == "model" {
				var input ModelInput
				input, err = ParseModel(v.Kind, v.body(t), v.DeclaredLength)
				if err == nil {
					data, _ := json.Marshal(input)
					json.Unmarshal(data, &normalized)
				}
			} else {
				var input TextInput
				if v.Type == "resume" {
					input, err = ParseResume(v.body(t), v.DeclaredLength)
				} else {
					input, err = ParsePrompt(v.Kind, v.body(t), v.DeclaredLength)
				}
				if err == nil {
					digest := sha256.Sum256([]byte(input.Text))
					normalized = map[string]any{"textHash": hex.EncodeToString(digest[:]), "revision": float64(input.Revision)}
				}
			}
			status := 200
			if err != nil {
				status = ErrorStatus(err)
			}
			if status != v.Status {
				t.Fatalf("Go input status %d differs from source %d", status, v.Status)
			}
			if err == nil && !reflect.DeepEqual(normalized, v.Normalized) {
				t.Fatal("Go normalized fields differ from unchanged legacy input reader")
			}
		})
	}
}
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	encode := func(value any) any {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err = json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	return reflect.DeepEqual(encode(a), encode(b))
}
func TestLegacyProfilePermissionsAndStorage(t *testing.T) {
	fixture := loadContract(t)
	if len(fixture.Permissions) != 135 || len(fixture.Mappings) != 4 {
		t.Fatal("source route/storage vectors missing")
	}
	for _, v := range fixture.Permissions {
		if got := AccessStatus(v.Surface, v.Kind, v.Method, true, v.Admin, v.Storage); got != v.Status {
			t.Fatalf("%s/%s %s actor %s storage %t: status %d, source %d", v.Surface, v.Kind, v.Method, v.User, v.Storage, got, v.Status)
		}
		if v.Owner != "" && v.Owner != v.User {
			t.Fatal("legacy resume route used a foreign owner")
		}
	}
	for _, v := range fixture.Mappings {
		p, err := PromptBinding(v.Kind)
		if err != nil || !sameJSON(t, p, v.Prompt) {
			t.Fatal("prompt storage mapping differs from actual source SQL")
		}
		m, err := ModelBinding(v.Kind)
		if err != nil || !sameJSON(t, m, v.Model) {
			t.Fatal("model storage mapping differs from actual source SQL")
		}
	}
	for _, surface := range []string{"resume", "prompts", "models"} {
		if AccessStatus(surface, "", "GET", false, true, true) != 401 {
			t.Fatal("anonymous profile route allowed")
		}
	}
	for _, kind := range []string{"CI_unknown", "relevance/other", "", "RELEVANCE"} {
		if _, err := PromptBinding(kind); ErrorStatus(err) != 404 {
			t.Fatal("unrecognized kind allowed into SQL mapping")
		}
		if _, err := ModelBinding(kind); ErrorStatus(err) != 404 {
			t.Fatal("unrecognized model kind allowed into SQL mapping")
		}
	}
}
func TestLegacyProfileDefaultsAndSchema(t *testing.T) {
	var source struct {
		Resume  Resume
		Prompts PromptCollection
		Models  map[string]Model
	}
	raw, err := os.ReadFile("defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, DefaultResume(), source.Resume) || !sameJSON(t, DefaultPrompts(), source.Prompts) || !sameJSON(t, DefaultModels(), source.Models) {
		t.Fatal("default projections differ from actual legacy readers")
	}
	prompts := DefaultPrompts()
	prompts.Prompts["relevance"] = Prompt{Markdown: "CI mutation"}
	prompts.MaxLengths["relevance"] = 1
	prompts.Defaults["vacancy_role"] = "CI mutation"
	models := DefaultModels()
	models["relevance"] = Model{Provider: "CI mutation"}
	if !sameJSON(t, DefaultResume(), source.Resume) || !sameJSON(t, DefaultPrompts(), source.Prompts) || !sameJSON(t, DefaultModels(), source.Models) {
		t.Fatal("caller mutated shared defaults")
	}
	schema := Schema()
	if schema.Version != 39 || len(schema.Tables) != 5 {
		t.Fatal("v39 profile schema incomplete")
	}
	expected := map[string][]string{
		"user_profiles":           {"user_name", "resume_md", "revision", "updated_at"},
		"system_prompts":          {"kind", "markdown", "revision", "updated_at"},
		"prompt_model_settings":   {"kind", "provider", "openai_model", "reasoning_effort", "revision", "updated_at", "openrouter_model"},
		"vacancy_detail_settings": {"id", "markdown", "prompt_revision", "provider", "openai_model", "reasoning_effort", "model_revision", "updated_at"},
		"vacancy_role_settings":   {"id", "markdown", "prompt_revision", "provider", "openai_model", "reasoning_effort", "model_revision", "updated_at", "openrouter_model"},
	}
	for _, table := range schema.Tables {
		var names []string
		for _, c := range table.Columns {
			names = append(names, c.Name)
		}
		if !reflect.DeepEqual(names, expected[table.Name]) {
			t.Fatal("profile table columns lost or reordered")
		}
	}
	schema.Tables[0].Columns[0].Name = "CI mutation"
	if Schema().Tables[0].Columns[0].Name == "CI mutation" {
		t.Fatal("caller mutated shared backup schema")
	}
}
