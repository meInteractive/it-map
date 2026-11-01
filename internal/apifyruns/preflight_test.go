package apifyruns

import (
	"context"
	"errors"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"testing"
)

func TestExecutionPreflightIsComposedScopedAndBeforeAdmission(t *testing.T) {
	denied := errors.New("CI pipeline changed")
	original := Service{}
	calls := []string{}
	guarded := original.WithCheck(func(context.Context) error { calls = append(calls, "owner"); return nil }).WithCheck(func(context.Context) error { calls = append(calls, "source"); return denied })
	v := apifysettings.OperationVersion{SourceURL: "https://hh.ru/employer/456"}
	if _, err := guarded.Execute(context.Background(), v, "CI guard", apify.HHOptions{}); !errors.Is(err, denied) || len(calls) != 2 || calls[0] != "owner" || calls[1] != "source" {
		t.Fatal("preflight admitted SQL/provider or lost composition", calls, err)
	}
	if _, err := original.Execute(context.Background(), v, "CI original", apify.HHOptions{}); !errors.Is(err, ErrStorage) || len(calls) != 2 {
		t.Fatal("scoped check mutated original service", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := guarded.Execute(ctx, v, "CI cancelled", apify.HHOptions{}); !errors.Is(err, context.Canceled) || len(calls) != 2 {
		t.Fatal("cancelled admission ran callback/provider", err)
	}
}
