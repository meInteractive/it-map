package channels

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestChannelInputLegacyContract(t *testing.T) {
	data, err := os.ReadFile("testdata/input-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Vectors []struct {
			Name       string
			Input      Input
			Current    *Channel
			Valid      bool
			Normalized NormalizedInput
			Error      string
		}
		Usernames []struct{ Input, Output string }
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Vectors) < 50 || len(fixture.Usernames) < 20 {
		t.Fatal("legacy channel vectors missing")
	}
	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := NormalizeInput(v.Input, v.Current)
			if !v.Valid {
				if !errors.Is(err, ErrInvalidInput) || err.Error() != v.Error {
					t.Fatal("legacy channel rejection/message differs")
				}
				return
			}
			if err != nil {
				t.Fatal("Go rejected valid legacy channel input")
			}
			if !reflect.DeepEqual(got, v.Normalized) {
				t.Fatal("channel normalization differs from unchanged legacy JS")
			}
		})
	}
	for i, v := range fixture.Usernames {
		if got := NormalizeUsername(v.Input); got != v.Output {
			t.Errorf("legacy username case %d differs", i)
		}
	}
}
