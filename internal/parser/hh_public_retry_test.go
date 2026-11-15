package parser

import (
	"errors"
	"fmt"
	"testing"

	"itmap/internal/integrations/hhpublic"
)

func TestPublicRetryDoesNotHideCleanupFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{hhpublic.ErrTemporary, true},
		{errors.Join(hhpublic.ErrTemporary, nil), true},
		{fmt.Errorf("context: %w", hhpublic.ErrTemporary), true},
		{errors.Join(hhpublic.ErrTemporary, errors.New("cleanup failure")), false},
		{nil, false},
	} {
		if got := onlyTemporaryPublicHH(tc.err); got != tc.want {
			t.Fatalf("retry classification %v = %v", tc.err, got)
		}
	}
}
