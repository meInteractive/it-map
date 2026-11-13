//go:build consumerevaluation

package consumermigration

import (
	"context"
	"testing"
)

func TestInitializeRejectsMissingInputs(t *testing.T) {
	if err := InitializeForEvaluation(nil, nil); err == nil {
		t.Fatal("missing inputs accepted")
	}
	if err := InitializeForEvaluation(context.Background(), nil); err == nil {
		t.Fatal("missing database accepted")
	}
}
