package parser

import (
	"encoding/json"
	"errors"
	"itmap/internal/aiproviders"
	"os"
	"reflect"
	"testing"
)

func checkLeafRolesSource(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Name, Content, Code, Message string
		OK                           bool
		Status                       int
		IDs                          []int64
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 15 {
		t.Fatal("source fixture")
	}
	allowed := []aiproviders.LeafRole{{ID: 2, Path: "CI / Two"}, {ID: 1, Path: "CI / One"}, {ID: 3, Path: "CI / Three"}}
	for _, f := range frames {
		t.Run(f.Name, func(t *testing.T) {
			ids, e := ValidateLeafRoleIDs(f.Content, allowed)
			if f.OK {
				if e != nil || !reflect.DeepEqual(ids, f.IDs) {
					t.Fatal("source result differs", e)
				}
				return
			}
			var typed *aiproviders.ServiceError
			if !errors.As(e, &typed) || typed.Code != f.Code || typed.Status != f.Status || typed.Message != f.Message {
				t.Fatal("source error differs", e)
			}
		})
	}
}
func TestLeafRolesSourceFixture(t *testing.T) {
	b, e := os.ReadFile("testdata/leaf-roles-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkLeafRolesSource(t, b)
}
