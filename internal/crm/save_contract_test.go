package crm

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestCompanySaveLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/save-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Name       string
			Input      SaveInput
			Valid      bool
			Normalized SaveInput
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) != 22 {
		t.Fatal("CRM source-backed vectors missing")
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			err := v.Input.Validate()
			if (err == nil) != v.Valid {
				t.Fatal("CRM validation differs from unchanged legacy route")
			}
			if v.Valid && !reflect.DeepEqual(v.Input, v.Normalized) {
				t.Fatal("CRM status/comment must not be trimmed or normalized")
			}
		})
	}
}
