package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/integrations/aihttp"
	"itmap/internal/profiles"
	"os"
	"reflect"
	"testing"
)

type decisionMock struct {
	routerMock
	catalog, response json.RawMessage
	payloads          *[]json.RawMessage
	err               error
}

func (m decisionMock) DecisionModels(context.Context) (json.RawMessage, error) { return m.catalog, nil }
func (m decisionMock) Decisions(_ context.Context, b json.RawMessage) (json.RawMessage, error) {
	*m.payloads = append(*m.payloads, append([]byte(nil), b...))
	return m.response, m.err
}
func jevStageService(response json.RawMessage, length int) (*RouterService, *[]json.RawMessage) {
	m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Revision: 1}}
	s := NewRouterService(m)
	calls := []json.RawMessage{}
	catalog, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"id": JEVModelID, "context_length": length, "pricing": map[string]string{"prompt": "0.01", "completion": "0"}}}})
	s.factory = func(string) (RouterTransport, error) {
		return decisionMock{catalog: catalog, response: response, payloads: &calls}, nil
	}
	return s, &calls
}
func checkJEVStagesSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Scenario struct {
			Task          string
			ContextLength int
		}
		Response, Result, Billing json.RawMessage
		Calls                     []struct {
			Payload json.RawMessage
			URL     string
		}
		OK            bool
		Code, Message string
		Status        int
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 16 {
		t.Fatal("source frames")
	}
	for i, f := range frames {
		length := f.Scenario.ContextLength
		if length == 0 {
			length = 262144
		}
		s, calls := jevStageService(f.Response, length)
		b := &billingProbe{}
		var got any
		var e error
		if f.Scenario.Task == "title_classification" {
			got, e = s.ClassifyTitles(context.Background(), "CI_TITLE", []TitleRole{{2, "CI_ROLE", "CI_RULES", 3}, {1, "CI_OTHER", "CI_OTHER_RULES", 4}}, b, nil)
		} else {
			r := GenerationRequest{Task: f.Scenario.Task, Instructions: "CI_INSTRUCTIONS", State: map[string]string{"text": "CI_VACANCY"}, Roles: []LeafRole{{2, "CI / Other"}, {1, "CI / Developer"}}}
			var v AIResult
			v, e = NewGenerator(nil, s).Automatic(context.Background(), r, profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev"}, b)
			if f.Scenario.Task == "relevance" {
				var content map[string]any
				_ = json.Unmarshal([]byte(v.Message.Content), &content)
				got = map[string]any{"model": v.Model, "relevance": content["relevance"], "content": v.Message.Content, "usage": v.Usage, "fallback_used": v.Fallback, "attempted_models": v.Attempted}
			} else {
				got = v
			}
		}
		if len(*calls) != len(f.Calls) {
			t.Fatalf("frame%d paid call count differs", i)
		}
		for n, c := range f.Calls {
			var a, w any
			_ = json.Unmarshal((*calls)[n], &a)
			_ = json.Unmarshal(c.Payload, &w)
			if !reflect.DeepEqual(a, w) {
				t.Fatalf("frame%d source payload differs", i)
			}
		}
		if !f.OK {
			var typed *ServiceError
			if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Message != f.Message {
				t.Fatalf("frame%d source error differs %v", i, e)
			}
			continue
		}
		if e != nil {
			t.Fatalf("frame%d %v", i, e)
		}
		var want, actual map[string]any
		_ = json.Unmarshal(f.Result, &want)
		v, _ := json.Marshal(got)
		_ = json.Unmarshal(v, &actual)
		// Leaf source сохраняет исходный answers; в общем AIResult содержится только нормализованный content.
		if f.Scenario.Task == "vacancy_role" {
			delete(want, "answers")
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("frame%d source result differs: %s want %s", i, v, f.Result)
		}
		var meta, expected any
		_ = json.Unmarshal(b.request, &meta)
		_ = json.Unmarshal(f.Billing, &expected)
		if !reflect.DeepEqual(meta, expected) {
			t.Fatalf("frame%d billing metadata differs", i)
		}
	}
}
func TestJEVStagesSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/jev-stages-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkJEVStagesSource(t, b)
}

func TestJEVLeafLimitAndBudgetGuards(t *testing.T) {
	roles := []LeafRole{}
	answers := map[string]any{}
	for i := int64(1); i <= 13; i++ {
		roles = append(roles, LeafRole{i, "CI_ROLE"})
		answers[fmt.Sprintf("leaf_%d", i)] = map[string]string{"type": "choice", "choice": "match"}
	}
	raw, _ := json.Marshal(map[string]any{"model": JEVModelID, "answers": answers, "usage": map[string]any{"cost": 0.001}})
	s, calls := jevStageService(raw, 262144)
	g := NewGenerator(nil, s)
	r := GenerationRequest{Task: "vacancy_role", Roles: roles, Instructions: "CI_RULES", State: "CI_FULL_TEXT"}
	c := profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev"}
	b := &billingProbe{deny: true}
	if _, e := g.Automatic(context.Background(), r, c, b); e == nil || len(*calls) != 0 {
		t.Fatal("budget denial sent POST")
	}
	b.deny = false
	_, e := g.Automatic(context.Background(), r, c, b)
	var service *ServiceError
	if !errors.As(e, &service) || service.Code != "invalid_output" || len(*calls) != 1 {
		t.Fatal("13 selected roles allowed", e)
	}
	r.CheckActive = func(context.Context) error { return errors.New("CI_JOB_CHANGED") }
	if _, e = g.Automatic(context.Background(), r, c, b); e == nil || len(*calls) != 1 {
		t.Fatal("inactive job sent POST")
	}
	r.CheckActive = nil
	r.Roles = append(roles, roles[0])
	if _, e = g.Automatic(context.Background(), r, c, b); e == nil || len(*calls) != 1 {
		t.Fatal("duplicate role sent POST")
	}
}
func TestJEVUnknownPaidOutcomeNoReplay(t *testing.T) {
	s, calls := jevStageService(nil, 262144)
	original := s.factory
	s.factory = func(key string) (RouterTransport, error) {
		v, e := original(key)
		m := v.(decisionMock)
		m.err = &aihttp.Error{Code: "network", Status: 502}
		return m, e
	}
	_, e := s.ClassifyTitles(context.Background(), "CI_TITLE", []TitleRole{{1, "CI_ROLE", "CI_RULES", 1}}, &billingProbe{}, nil)
	if e == nil || len(*calls) != 1 {
		t.Fatal("uncertain paid request repeated", e)
	}
}

func TestJEVLeafValidationErrorStopsResultWithoutReplay(t *testing.T) {
	raw := json.RawMessage(`{"model":"typesafe/jev-1.13","answers":{"leaf_1":{"type":"choice","choice":"match"}},"usage":{"cost":0.001}}`)
	s, calls := jevStageService(raw, 262144)
	bill := &billingProbe{}
	expected := &ServiceError{Code: "invalid_output", Status: 422, Message: "CI_VALIDATION_FAILED"}
	r := GenerationRequest{Task: "vacancy_role", Instructions: "CI_RULES", State: "CI_TEXT", Roles: []LeafRole{{ID: 1, Path: "CI_ROLE"}}, ValidateContent: func(string) error { return expected }}
	_, e := NewGenerator(nil, s).Automatic(context.Background(), r, profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev"}, bill)
	if !errors.Is(e, expected) || len(*calls) != 1 || bill.calls != 1 {
		t.Fatal("validation failure was lost or POST repeated", e)
	}
}
