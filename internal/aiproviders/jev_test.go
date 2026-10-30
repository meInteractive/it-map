package aiproviders

import (
	"context"
	"encoding/json"
	"errors"
	"itmap/internal/profiles"
	"os"
	"reflect"
	"testing"
	"time"
)

type jevMock struct {
	routerMock
	models json.RawMessage
	posts  *int
}

func (m jevMock) DecisionModels(context.Context) (json.RawMessage, error) { return m.models, nil }
func (m jevMock) Decisions(context.Context, json.RawMessage) (json.RawMessage, error) {
	*m.posts++
	return []byte(`{"model":"typesafe/jev-1.13","answers":{"role_1":{"type":"choice","choice":"match"}},"usage":{"cost":0.001}}`), nil
}
func checkJEVCatalogSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Models        []map[string]any
		URL           string
		OK            bool
		Result        map[string]any
		Code, Message string
		Status        int
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 3 {
		t.Fatal("source corpus")
	}
	for _, f := range frames {
		m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Revision: 1}}
		s := NewRouterService(m)
		posts := 0
		b, _ := json.Marshal(map[string]any{"data": f.Models})
		s.factory = func(string) (RouterTransport, error) { return jevMock{models: b, posts: &posts}, nil }
		v, e := s.DecisionCatalog(context.Background())
		if f.OK {
			if e != nil {
				t.Fatal(e)
			}
			if _, e = time.Parse(time.RFC3339, v["fetchedAt"].(string)); e != nil {
				t.Fatal(e)
			}
			delete(v, "fetchedAt")
			a, _ := json.Marshal(v)
			var got map[string]any
			_ = json.Unmarshal(a, &got)
			if !reflect.DeepEqual(got, f.Result) {
				t.Fatal("source catalog differs")
			}
		} else {
			var typed *ServiceError
			if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Error() != f.Message {
				t.Fatal("source error differs", e)
			}
		}
		if posts != 0 {
			t.Fatal("catalog sent paid POST")
		}
	}
}
func TestJEVCatalogSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/jev-catalog-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkJEVCatalogSource(t, b)
}
func TestJEVRequiresBillingBeforePOST(t *testing.T) {
	m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Revision: 1}}
	s := NewRouterService(m)
	posts := 0
	s.factory = func(string) (RouterTransport, error) {
		return jevMock{models: []byte(`{"data":[{"id":"typesafe/jev-1.13","context_length":262144,"pricing":{"prompt":"0.01","completion":"0"}}]}`), posts: &posts}, nil
	}
	q := map[string]any{"role_1": map[string]any{"type": "choice"}}
	if _, e := s.AutomaticDecisions(context.Background(), nil, map[string]any{"title": "CI"}, q); e == nil || posts != 0 {
		t.Fatal("unbilled POST")
	}
	b := &billingProbe{deny: true}
	if _, e := s.AutomaticDecisions(context.Background(), b, map[string]any{"title": "CI"}, q); e == nil || posts != 0 {
		t.Fatal("denied budget sent POST")
	}
	b.deny = false
	if _, e := s.AutomaticDecisions(context.Background(), b, map[string]any{"title": "CI"}, q); e != nil || posts != 1 {
		t.Fatal("admitted JEV POST failed", e)
	}
	var meta map[string]any
	_ = json.Unmarshal(b.request, &meta)
	if meta["model"] != JEVModelID || meta["pricing"] == nil || meta["contextLength"] == nil {
		t.Fatal("paid cost metadata missing")
	}
}

func TestJEVProfileCatalogChoice(t *testing.T) {
	m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Revision: 1}}
	s := NewRouterService(m)
	posts := 0
	s.factory = func(string) (RouterTransport, error) {
		return jevMock{models: []byte(`{"data":[{"id":"typesafe/jev-1.13","pricing":{"prompt":"0.01"}}]}`), posts: &posts}, nil
	}
	c := ModelCatalog{Router: s}
	if e := c.ValidateChoice(context.Background(), "relevance", profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev", ReasoningEffort: "auto"}); e != nil {
		t.Fatal(e)
	}
	if e := c.ValidateChoice(context.Background(), "cover_letter", profiles.ModelInput{Provider: "openrouter", OpenRouterModel: "jev", ReasoningEffort: "auto"}); e == nil {
		t.Fatal("JEV cover letter admitted")
	}
	if posts != 0 {
		t.Fatal("saving model sent paid POST")
	}
}

type jevFenceBilling struct {
	before func()
	calls  int
}

func (b *jevFenceBilling) Run(ctx context.Context, _ json.RawMessage, work func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	b.calls++
	b.before()
	return work(ctx)
}
func TestJEVRotationAndRestoreBeforePaidPOST(t *testing.T) {
	for _, restore := range []bool{false, true} {
		m := &routerMemory{generation: "CI_GENERATION", key: "CI_KEY", row: RouterSnapshot{Configured: true, Revision: 1}}
		s := NewRouterService(m)
		posts := 0
		s.factory = func(string) (RouterTransport, error) {
			return jevMock{models: []byte(`{"data":[{"id":"typesafe/jev-1.13","pricing":{"prompt":"0.01"}}]}`), posts: &posts}, nil
		}
		b := &jevFenceBilling{before: func() {
			if restore {
				m.generation = "RESTORED"
			} else {
				m.row.Revision++
			}
		}}
		_, e := s.AutomaticDecisions(context.Background(), b, "CI_STATE", map[string]any{"q": map[string]any{"type": "score"}})
		want := ErrChanged
		if restore {
			want = ErrGeneration
		}
		if !errors.Is(e, want) || posts != 0 || b.calls != 1 {
			t.Fatal("late change allowed POST", e, posts)
		}
	}
}
