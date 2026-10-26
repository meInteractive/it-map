package tasks

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestTaskCreateLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/create-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Name       string
			Input      CreateInput
			Valid      bool
			Normalized CreateInput
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) < 25 {
		t.Fatal("legacy task vectors missing")
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := v.Input.normalize()
			if !v.Valid {
				if err == nil {
					t.Fatal("Go accepted a task rejected by legacy validation")
				}
				return
			}
			if err != nil {
				t.Fatal("Go rejected a valid legacy task")
			}
			if !reflect.DeepEqual(got, v.Normalized) {
				t.Fatal("task normalization differs from unchanged legacy JS")
			}
		})
	}
}
