//go:build mariadb && schedulerwrites && schedulersettings

package storage

import (
	"errors"
	"reflect"
	"testing"

	"itmap/internal/parser"
)

func TestMariaDBSchedulerSettingsHHRoleSelectionsDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := openSettingsSQL(t)
	f.exec("INSERT INTO vacancy_roles(id,stable_key,name,sort_order) VALUES(991801,'ci-hh-role-it','CI IT',1),(991802,'ci-hh-role-news','CI News',2)")
	t.Cleanup(func() { f.exec("DELETE FROM vacancy_roles WHERE id IN (991801,991802)") })
	repo := parser.HHRoleSettingsRepository{DB: f.db}
	initial, e := repo.Read(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	first, e := repo.Save(f.ctx, initial.Generation, 991801, 0, []string{"107", "36"})
	if e != nil || first.Revision != 1 {
		t.Fatalf("first=%+v err=%v", first, e)
	}
	second, e := repo.Save(f.ctx, initial.Generation, 991802, 0, []string{"40"})
	if e != nil || second.Revision != 1 {
		t.Fatalf("second=%+v err=%v", second, e)
	}
	if _, e = repo.Save(f.ctx, initial.Generation, 991801, 0, []string{"36"}); !errors.Is(e, parser.ErrSettingsChanged) {
		t.Fatalf("stale write=%v", e)
	}
	after, e := repo.Read(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	found := map[int64]parser.HHRoleSetting{}
	for _, v := range after.Roles {
		found[v.RoleID] = v
	}
	if !reflect.DeepEqual(found[991801].RoleIDs, []string{"36", "107"}) || !reflect.DeepEqual(found[991802].RoleIDs, []string{"40"}) {
		t.Fatalf("roles=%+v", found)
	}
	if _, e = repo.Save(f.ctx, "00000000-0000-0000-0000-000000000000", 991801, 1, []string{"36"}); !errors.Is(e, parser.ErrSettingsGeneration) {
		t.Fatalf("generation=%v", e)
	}
}
