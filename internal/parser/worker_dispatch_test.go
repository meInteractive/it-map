package parser

import (
	"context"
	"errors"
	"testing"
)

type workerTestStages struct{ calls []string }

func TestWorkerTargetedCycleBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		worker Worker
		max    int
		want   bool
	}{
		{"обычный цикл", Worker{}, 50, true},
		{"одно задание", Worker{TargetJobID: "CI_JOB"}, 1, true},
		{"несколько заданий", Worker{TargetJobID: "CI_JOB"}, 2, false},
		{"планирование источников", Worker{TargetJobID: "CI_JOB", ScheduleSources: true}, 1, false},
		{"старый dataset", Worker{TargetJobID: "CI_JOB", ResumePreviousDataset: true}, 1, false},
		{"два идентификатора", Worker{TargetJobID: "CI_JOB", ResumeJobID: "CI_OTHER"}, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validTargetedCycle(tc.worker, WorkerOptions{MaxJobs: tc.max}); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

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

type workerTestListing struct {
	done bool
	code string
}

func (s workerTestListing) Execute(context.Context, *JobLease) (HHListingProgress, error) {
	return HHListingProgress{Done: s.done, PartialCode: s.code}, nil
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

func TestWorkerClaimRestrictsSource(t *testing.T) {
	hh, other := "hh", "companySite"
	options := ClaimOptions{SourceID: &hh}
	types := map[string]bool{"import_vacancy": true}
	for _, test := range []struct {
		source  *string
		allowed bool
	}{{&hh, true}, {&other, false}, {nil, false}} {
		job := Job{Status: "pending", Type: "import_vacancy", SourceID: test.source}
		allowed, _, err := claimEligible(job, 1, DefaultSettings(), options, types, claimView{})
		if err != nil || allowed != test.allowed {
			t.Fatalf("source %v: %v %v", test.source, allowed, err)
		}
	}
}

func TestWorkerDispatchRetainsListingDiagnostic(t *testing.T) {
	d := workerDispatch{listing: workerTestListing{done: true, code: "apify_partial_data"}}
	result, err := d.execute(context.Background(), &JobLease{job: Job{Type: "scan_source"}})
	if err != nil || result.pending || result.code != "apify_partial_data" {
		t.Fatalf("partial result: %+v %v", result, err)
	}
}

type workerFailListing struct{ err error }

func (s workerFailListing) Execute(context.Context, *JobLease) (HHListingProgress, error) {
	return HHListingProgress{}, s.err
}

type workerFailDetail struct{ err error }

func (s workerFailDetail) Execute(context.Context, *JobLease) (HHFullTextResult, error) {
	return HHFullTextResult{}, s.err
}

type workerFailImport struct{ err error }

func (s workerFailImport) Execute(context.Context, *JobLease) (HHImportResult, error) {
	return HHImportResult{}, s.err
}

type workerFailAI struct{ err error }

func (s workerFailAI) Execute(context.Context, *JobLease) (AIStageOutput, error) {
	return AIStageOutput{}, s.err
}
func TestWorkerDispatchPropagatesStageFailures(t *testing.T) {
	failure := errors.New("CI stage failed")
	d := workerDispatch{listing: workerFailListing{failure}, detail: workerFailDetail{failure}, importer: workerFailImport{failure}, ai: workerFailAI{failure}}
	for _, kind := range []string{"scan_source", "classify_title", "fetch_full_text", "extract_vacancy", "assign_roles", "import_vacancy", "score_relevance"} {
		result, err := d.execute(context.Background(), &JobLease{job: Job{Type: kind}})
		if !errors.Is(err, failure) || result.finished {
			t.Fatalf("%s: %+v %v", kind, result, err)
		}
	}
}
