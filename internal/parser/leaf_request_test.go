package parser

import (
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/profiles"
	"os"
	"reflect"
	"strings"
	"testing"
)

func checkLeafRequestSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Name, Provider, Title, Markdown, Prompt, Code, Message string
		Allowed                                                []aiproviders.LeafRole
		Value, Answers, Payload                                json.RawMessage
		OK                                                     bool
		Status                                                 int
		IDs                                                    []int64
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 4 {
		t.Fatal("source corpus")
	}
	for _, f := range frames {
		t.Run(f.Name, func(t *testing.T) {
			cfg := profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", OpenRouterModel: "best_free", ReasoningEffort: "medium"}
			if f.Provider == "jev" {
				cfg.Provider = "openrouter"
				cfg.OpenRouterModel = "jev"
			}
			req, e := PrepareLeafRoleRequest(f.Prompt, f.Title, f.Markdown, f.Allowed, cfg)
			if e != nil || req.Task != "vacancy_role" || req.MaxTokens != 4096 {
				t.Fatal("request invalid", e)
			}
			var payload map[string]json.RawMessage
			_ = json.Unmarshal(f.Payload, &payload)
			if f.Provider == "openai" {
				var source []aiproviders.Message
				_ = json.Unmarshal(payload["input"], &source)
				msgs := append([]aiproviders.Message(nil), req.Messages...)
				msgs[0].Role = "developer"
				if !reflect.DeepEqual(msgs, source) {
					t.Fatal("source messages differ")
				}
				var format struct {
					Type string         `json:"type"`
					Spec map[string]any `json:"json_schema"`
				}
				_ = json.Unmarshal(req.ResponseFormat, &format)
				format.Spec["type"] = format.Type
				var text struct{ Format map[string]any }
				_ = json.Unmarshal(payload["text"], &text)
				if !reflect.DeepEqual(format.Spec, text.Format) {
					t.Fatal("source schema differs")
				}
				e = req.ValidateContent(string(f.Value))
				if f.OK && e != nil {
					t.Fatal(e)
				}
				if !f.OK {
					var typed *aiproviders.ServiceError
					if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Message != f.Message {
						t.Fatal("source validator differs", e)
					}
				}
			} else {
				var state map[string]any
				_ = json.Unmarshal(payload["state"], &state)
				stateBytes, _ := json.Marshal(req.State)
				var actual any
				_ = json.Unmarshal(stateBytes, &actual)
				if req.Instructions != f.Prompt || !reflect.DeepEqual(req.Roles, f.Allowed) || !reflect.DeepEqual(actual, state["vacancy"]) {
					t.Fatal("source JEV input differs")
				}
			}
		})
	}
}
func TestLeafRequestSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/leaf-role-request-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkLeafRequestSource(t, b)
}
func TestLeafRequestPreflight(t *testing.T) {
	cfg := profiles.ModelInput{Provider: "openai", OpenRouterModel: "best_free", ReasoningEffort: "auto"}
	roles := []aiproviders.LeafRole{{ID: 1, Path: "CI"}}
	for _, x := range []struct {
		prompt, raw string
		roles       []aiproviders.LeafRole
		code        string
	}{{"p", "x", nil, "no_leaf_roles"}, {"p", strings.Repeat("x", 120001), roles, "input_too_large"}, {" ", "x", roles, "empty_prompt"}} {
		_, e := PrepareLeafRoleRequest(x.prompt, "CI", x.raw, x.roles, cfg)
		var v *aiproviders.ServiceError
		if !errors.As(e, &v) || v.Code != x.code {
			t.Fatal("preflight accepted", e)
		}
	}
}
