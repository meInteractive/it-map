package crm

import (
	"context"
	"testing"
)

func TestRejectInvalidFlag(t *testing.T) {
	r := Repository{}
	if _, err := r.CompanyNames(context.Background(), "Demo", "favorite"); err == nil {
		t.Fatal("nil DB accepted")
	}
}
