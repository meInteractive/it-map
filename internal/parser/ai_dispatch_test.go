package parser

import (
	"context"
	"errors"
	"testing"
)

func TestAIStageDispatcherRouting(t *testing.T) {
	d := AIStageDispatcher{Owner: "synthetic-user"}
	for _, test := range []struct {
		kind string
		want any
	}{
		{"classify_title", TitleStageStore{Owner: d.Owner}},
		{"extract_vacancy", ExtractionStageStore{Owner: d.Owner}},
		{"assign_roles", LeafStageStore{Owner: d.Owner}},
	} {
		t.Run(test.kind, func(t *testing.T) {
			executor, err := d.executor(test.kind)
			if err != nil || executor.Store != test.want {
				t.Fatalf("wrong stage: %v, %v", executor, err)
			}
			planner, ok := executor.Handoff.Planner.(SQLCandidateHandoffPlanner)
			if !ok || planner.Owner != d.Owner {
				t.Fatal("handoff must use the same server owner")
			}
		})
	}
	for _, kind := range []string{"", "fetch_full_text", "import_vacancy", "score_relevance"} {
		if _, err := d.executor(kind); !errors.Is(err, ErrAIStageUnavailable) {
			t.Fatalf("unsupported type %q: %v", kind, err)
		}
	}
	if _, err := (AIStageDispatcher{}).executor("assign_roles"); !errors.Is(err, ErrAIStageUnavailable) {
		t.Fatal("missing owner accepted")
	}
	if _, err := d.Execute(context.Background(), nil); !errors.Is(err, ErrAIStageUnavailable) {
		t.Fatal("nil lease accepted")
	}
}
