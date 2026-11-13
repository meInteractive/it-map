package backup

import (
	"context"
	"testing"
)

func TestConsumerCandidateExportRejectsMissingInput(t *testing.T) {
	if _, err := (Repository{}).ExportConsumerCandidateForEvaluation(nil, ""); err == nil {
		t.Fatal("missing context and database accepted")
	}
	if _, err := (Repository{}).ExportConsumerCandidateForEvaluation(context.Background(), "11111111-2222-4333-8444-555555555555"); err == nil {
		t.Fatal("missing database accepted")
	}
}
