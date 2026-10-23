package vacancies

import (
	"errors"
	"itmap/internal/roles"
	"testing"
)

func TestManualRoleSelection(t *testing.T) {
	dir := roles.Catalog{Roles: []roles.Role{
		{ID: 1, Name: "IT Manager", Path: "IT Manager", IsLeaf: false},
		{ID: 2, Name: "Менеджер продуктов", Path: "IT Manager → Менеджер продуктов", IsLeaf: true},
		{ID: 3, Name: "Руководитель разработки", Path: "IT Manager → Руководитель разработки", IsLeaf: true},
	}}
	for _, tc := range []struct {
		input   Input
		ids     []int64
		name    string
		wantErr bool
	}{
		{input: Input{RoleIDs: []int64{3, 3, 2}}, ids: []int64{3, 2}, name: "Руководитель разработки; Менеджер продуктов"},
		{input: Input{Role: "Менеджер продукта; Руководитель разработки"}, ids: []int64{2, 3}, name: "Менеджер продуктов; Руководитель разработки"},
		{input: Input{Role: "IT Manager → Менеджер продуктов"}, ids: []int64{2}, name: "Менеджер продуктов"},
		{input: Input{RoleIDs: []int64{1}}, wantErr: true},
		{input: Input{RoleIDs: []int64{77}}, wantErr: true},
		{input: Input{Role: "Неизвестная"}, wantErr: true},
		{input: Input{RoleIDs: []int64{}}, wantErr: true},
	} {
		ids, name, err := resolveManualRoles(tc.input, dir)
		if tc.wantErr {
			if !errors.Is(err, ErrInvalidRole) {
				t.Fatalf("%+v: %v", tc.input, err)
			}
			continue
		}
		if err != nil || name != tc.name || len(ids) != len(tc.ids) {
			t.Fatalf("%+v: ids=%v name=%q err=%v", tc.input, ids, name, err)
		}
		for i := range ids {
			if ids[i] != tc.ids[i] {
				t.Fatalf("order mismatch: %v", ids)
			}
		}
	}
}

func TestCanonicalTelegramMessage(t *testing.T) {
	for _, input := range []string{"t.me/channel_test/123", "https://telegram.me/s/channel_test/123?utm_source=x", "https://t.me/channel_test/123"} {
		result, err := CanonicalTelegramMessage(input, "channel_test")
		if err != nil || result != "https://t.me/channel_test/123" {
			t.Fatalf("input=%s got=%q error=%v", input, result, err)
		}
	}
	for _, input := range []string{"https://bad.example/channel_test/123", "https://t.me/channel_test", "https://t.me/othername/123", "https://t.me/channel_test/hello", "https://user@t.me/channel_test/123", "https://t.me/channel_test/12/garbage"} {
		if _, err := CanonicalTelegramMessage(input, "channel_test"); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
