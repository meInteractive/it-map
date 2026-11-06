package parser

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestHHRoleSelectionsAreIndependent(t *testing.T) {
	raw := []byte(`{"revision":4,"cloudOwnerId":"private","future":{"enabled":true}}`)
	first, it, err := updateHHRoleSelection(raw, 10, 0, []string{"107", "36"})
	if err != nil || it.Revision != 1 || !reflect.DeepEqual(it.RoleIDs, []string{"36", "107"}) {
		t.Fatalf("first=%v err=%v", it, err)
	}
	second, journalist, err := updateHHRoleSelection(first, 11, 0, []string{"40"})
	if err != nil || journalist.Revision != 1 {
		t.Fatalf("second=%v err=%v", journalist, err)
	}
	var state struct {
		CloudOwnerID string                     `json:"cloudOwnerId"`
		Future       map[string]bool            `json:"future"`
		Roles        map[string]HHRoleSelection `json:"hhProfessionalRoles"`
	}
	if json.Unmarshal(second, &state) != nil || state.CloudOwnerID != "private" || !state.Future["enabled"] || !reflect.DeepEqual(state.Roles["10"], it) {
		t.Fatalf("state=%+v", state)
	}
	if _, _, err = updateHHRoleSelection(second, 10, 0, []string{"36"}); !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("stale write: %v", err)
	}
	for _, bad := range [][]string{nil, {"0"}, {"01"}, {"36", "36"}, {"1&text=x"}} {
		if _, _, err = updateHHRoleSelection(second, 10, 1, bad); !errors.Is(err, ErrHHRoleSettings) {
			t.Errorf("accepted %q: %v", bad, err)
		}
	}
}
