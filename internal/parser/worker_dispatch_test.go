package parser

import (
	"context"
	"errors"
	"testing"
)

type workerTestStages struct{ calls []string }

func (s *workerTestStages) Execute(ctx context.Context, j *JobLease) (AIStageOutput, error) {
	s.calls = append(s.calls, j.Job().Type)
	return AIStageOutput{}, nil
}
func TestWorkerDispatchAIAndUnsupported(t *testing.T) {
	s := &workerTestStages{}
	d := workerDispatch{ai: s}
	for _, kind := range []string{"classify_title", "extract_vacancy", "assign_roles", "score_relevance"} {
		result, err := d.execute(context.Background(), &JobLease{job: Job{Type: kind}})
		if err != nil || !result.finished || result.pending {
			t.Fatalf("%s: %+v %v", kind, result, err)
		}
	}
	if len(s.calls) != 4 {
		t.Fatalf("AI calls: %v", s.calls)
	}
	if _, err := d.execute(context.Background(), &JobLease{job: Job{Type: "diagnostic"}}); !errors.Is(err, ErrWorkerUnsupported) {
		t.Fatalf("unsupported: %v", err)
	}
	if _, err := d.execute(context.Background(), nil); !errors.Is(err, ErrJobInput) {
		t.Fatalf("nil: %v", err)
	}
}

type workerTestListing struct{ done bool }

func (s workerTestListing) Execute(context.Context, *JobLease) (HHListingProgress, error) {
	return HHListingProgress{Done: s.done}, nil
}

type workerTestDetail struct{ pending bool }

func (s workerTestDetail) Execute(context.Context, *JobLease) (HHFullTextResult, error) {
	return HHFullTextResult{Pending: s.pending}, nil
}

type workerTestImport struct{}

func (workerTestImport) Execute(context.Context, *JobLease) (HHImportResult, error) {
	return HHImportResult{}, nil
}
func TestWorkerDispatchNonAIProgress(t *testing.T) {
	for _, test := range []struct {
		kind    string
		d       workerDispatch
		pending bool
	}{
		{"scan_source", workerDispatch{listing: workerTestListing{}}, true},
		{"scan_source", workerDispatch{listing: workerTestListing{done: true}}, false},
		{"fetch_full_text", workerDispatch{detail: workerTestDetail{pending: true}}, true},
		{"fetch_full_text", workerDispatch{detail: workerTestDetail{}}, false},
		{"import_vacancy", workerDispatch{importer: workerTestImport{}}, false},
	} {
		result, err := test.d.execute(context.Background(), &JobLease{job: Job{Type: test.kind}})
		if err != nil || result.pending != test.pending || result.finished {
			t.Fatalf("%s: %+v %v", test.kind, result, err)
		}
	}
}
