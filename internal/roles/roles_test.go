package roles

import (
	"encoding/json"
	"strings"
	"testing"
)

func parent(id int64) *int64 { return &id }
func TestBuildRoleTree(t *testing.T) {
	c, err := Build([]Role{
		{ID: 1, StableKey: "it_manager", Name: "IT Manager", SortOrder: 0, ChildCount: 2},
		{ID: 2, StableKey: "project", Name: "Менеджер проектов", ParentID: parent(1), SortOrder: 2, UsageCount: 5},
		{ID: 3, StableKey: "devlead", Name: "Руководитель разработки", ParentID: parent(1), SortOrder: 1, UsageCount: 3},
		{ID: 4, StableKey: "journalist", Name: "Журналист", SortOrder: 1, ChildCount: 1},
		{ID: 5, StableKey: "editor", Name: "Редактор", ParentID: parent(4)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Roles) != 5 || len(c.Roots) != 2 || len(c.Leaves) != 3 || len(c.Tree) != 2 {
		t.Fatalf("wrong role counts: %+v", c)
	}
	root := c.Tree[0]
	if root.ID != 1 || len(root.Children) != 2 || root.Children[0].ID != 3 {
		t.Fatalf("bad tree sorting %+v", root)
	}
	if c.Roles[1].Path != "IT Manager → Менеджер проектов" || c.Roles[1].IsLeaf != true || len(c.Roles[1].DescendantIDs) != 0 {
		t.Fatalf("wrong role metadata %+v", c.Roles[1])
	}
	if len(c.Roles[0].DescendantIDs) != 2 || c.Roles[0].DescendantIDs[0] != 3 {
		t.Fatalf("descendants %+v", c.Roles[0])
	}
	b, _ := json.Marshal(c)
	for _, field := range []string{`"roles"`, `"roots"`, `"leaves"`, `"tree"`, `"children"`, `"descendantIds"`, `"stableKey"`} {
		if !strings.Contains(string(b), field) {
			t.Fatalf("missing field %s", field)
		}
	}
}
func TestBuildRejectsCorruptGraph(t *testing.T) {
	for _, tc := range []struct {
		label string
		roles []Role
	}{
		{"duplicate", []Role{{ID: 1}, {ID: 1}}},
		{"orphan", []Role{{ID: 1, ParentID: parent(100)}}},
		{"cycle", []Role{{ID: 1, ParentID: parent(2)}, {ID: 2, ParentID: parent(1)}}},
		{"self", []Role{{ID: 1, ParentID: parent(1)}}},
		{"bad ID", []Role{{ID: 0}}},
	} {
		if _, err := Build(tc.roles); err == nil {
			t.Errorf("accepted %s", tc.label)
		}
	}
}
func TestEmptyRoleCatalogNotNull(t *testing.T) {
	c, err := Build(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(c)
	if string(b) != `{"roles":[],"tree":[],"roots":[],"leaves":[]}` {
		t.Fatalf("unexpected empty response %s", b)
	}
}
