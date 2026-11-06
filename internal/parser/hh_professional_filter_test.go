package parser

import (
	"errors"
	"reflect"
	"testing"
)

func TestHHProfessionalRoleUnion(t *testing.T) {
	raw := []byte(`{"hhProfessionalRoles":{"1":{"revision":1,"roleIds":["107","36"]},"2":{"revision":2,"roleIds":["36","73"]},"3":{"revision":1,"roleIds":[]}}}`)
	got, e := hhProfessionalRoleUnion(raw, []int64{1, 2})
	if e != nil || !reflect.DeepEqual(got, []string{"36", "73", "107"}) {
		t.Fatalf("got %v, error %v", got, e)
	}
	for _, roots := range [][]int64{nil, {1, 3}, {1, 4}} {
		if _, e = hhProfessionalRoleUnion(raw, roots); !errors.Is(e, ErrHHRoleFilter) {
			t.Fatalf("roots %v, error %v", roots, e)
		}
	}
}
