package parser

import (
	"context"
	"errors"
	"testing"
)

func TestCompanySiteFullTextDispatch(t *testing.T) {
	source := "companySite"
	lease := &JobLease{job: Job{Type: "fetch_full_text", SourceID: &source}}
	dispatch := workerDispatch{detail: workerTestDetail{}}
	if _, err := dispatch.execute(context.Background(), lease); !errors.Is(err, ErrAIStageUnavailable) {
		t.Fatalf("company site reached HH executor: %v", err)
	}
	dispatch.firecrawlDetail = workerTestDetail{pending: true}
	result, err := dispatch.execute(context.Background(), lease)
	if err != nil || !result.pending {
		t.Fatalf("company site executor result = %+v, %v", result, err)
	}
}

func TestSameFirecrawlPage(t *testing.T) {
	const job = "https://careers.company.com/job?id=42"
	for _, test := range []struct {
		final string
		want  bool
	}{
		{"https://CAREERS.company.com/job/?id=42", true},
		{"https://careers.company.com/job?id=43", false},
		{"https://careers.company.com/jobs", false},
		{"https://other.company.com/job?id=42", false},
	} {
		if got := sameFirecrawlPage(job, test.final); got != test.want {
			t.Errorf("same page for %q = %t", test.final, got)
		}
	}
}
