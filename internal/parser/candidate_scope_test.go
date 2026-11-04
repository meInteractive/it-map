package parser

import "testing"

func TestCandidateScopeMatchesOnlyRecordedCompanyAndSource(t *testing.T) {
	company, other, candidate, foreign := int64(1), int64(2), int64(10), int64(20)
	hh, otherSource := "hh", "companySite"
	scope := &ScanCandidateScope{CompanyID: company}
	ids := map[int64]bool{candidate: true}
	tests := []struct {
		name string
		job  Job
		want bool
	}{
		{"selected", Job{Type: "classify_title", CompanyID: &company, SourceID: &hh, CandidateID: &candidate}, true},
		{"different candidate", Job{Type: "classify_title", CompanyID: &company, SourceID: &hh, CandidateID: &foreign}, false},
		{"different company", Job{Type: "classify_title", CompanyID: &other, SourceID: &hh, CandidateID: &candidate}, false},
		{"different source", Job{Type: "classify_title", CompanyID: &company, SourceID: &otherSource, CandidateID: &candidate}, false},
		{"scan", Job{Type: "scan_source", CompanyID: &company, SourceID: &hh, CandidateID: &candidate}, false},
		{"relevance", Job{Type: "score_relevance", CompanyID: &company, SourceID: &hh, CandidateID: &candidate}, false},
		{"missing candidate", Job{Type: "extract_vacancy", CompanyID: &company, SourceID: &hh}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := inCandidateScope(tc.job, scope, ids); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
	if !inCandidateScope(Job{}, nil, nil) {
		t.Fatal("ordinary queue changed")
	}
}
