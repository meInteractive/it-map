package consumercontrol

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestResetForRestoreRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		ctx        context.Context
		tx         *sql.Tx
		generation string
	}{
		{nil, nil, ""}, {context.Background(), nil, "11111111-2222-4333-8444-555555555555"},
		{context.Background(), new(sql.Tx), "invalid"},
	} {
		if _, err := ResetForRestore(test.ctx, test.tx, test.generation); !errors.Is(err, ErrState) {
			t.Fatal("invalid restore input accepted", err)
		}
	}
}
