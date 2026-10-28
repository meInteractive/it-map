// Package roles provides read-only vacancy-role projections for the migration.
package roles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"unicode/utf16"

	"itmap/internal/rucollation"
)

type Role struct {
	ID            int64   `json:"id"`
	StableKey     string  `json:"stableKey"`
	Name          string  `json:"name"`
	ParentID      *int64  `json:"parentId"`
	SortOrder     int64   `json:"sortOrder"`
	UsageCount    int64   `json:"usageCount"`
	ChildCount    int64   `json:"childCount"`
	IsLeaf        bool    `json:"isLeaf"`
	Path          string  `json:"path"`
	DescendantIDs []int64 `json:"descendantIds"`
}

type Node struct {
	Role
	Children []Node `json:"children"`
}

type Catalog struct {
	Roles  []Role `json:"roles"`
	Tree   []Node `json:"tree"`
	Roots  []Role `json:"roots"`
	Leaves []Role `json:"leaves"`
}

type Repository struct{ DB *sql.DB }

func (r Repository) Read(ctx context.Context) (Catalog, error) {
	if r.DB == nil {
		return Catalog{}, errors.New("vacancy role database unavailable")
	}
	const q = `SELECT r.id,r.stable_key,r.name,r.parent_id,r.sort_order,
 (SELECT COUNT(DISTINCT a.vacancy_id) FROM vacancy_role_assignments AS a WHERE a.role_id=r.id) AS usageCount,
 (SELECT COUNT(*) FROM vacancy_roles AS child WHERE child.parent_id=r.id) AS childCount
 FROM vacancy_roles AS r
 ORDER BY (r.parent_id IS NOT NULL),r.parent_id,r.sort_order,r.id`
	rows, err := r.DB.QueryContext(ctx, q)
	if err != nil {
		return Catalog{}, err
	}
	defer rows.Close()
	list := make([]Role, 0)
	for rows.Next() {
		var v Role
		var parent sql.NullInt64
		if err := rows.Scan(&v.ID, &v.StableKey, &v.Name, &parent, &v.SortOrder, &v.UsageCount, &v.ChildCount); err != nil {
			return Catalog{}, err
		}
		if parent.Valid {
			p := parent.Int64
			v.ParentID = &p
		}
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return Catalog{}, err
	}
	return Build(list)
}

// Build mirrors the role-tree contract in server/repositories/vacancy-roles.js.
// Unlike the legacy recursive implementation, invalid cycles and missing
// parent references fail instead of risking infinite recursion or lost nodes.
func Build(raw []Role) (Catalog, error) {
	result := Catalog{Roles: make([]Role, 0, len(raw)), Tree: []Node{}, Roots: []Role{}, Leaves: []Role{}}
	byID := make(map[int64]*Role, len(raw))
	children := map[int64][]int64{}
	keys := map[int64]rucollation.Key{}
	var order []int64
	for _, input := range raw {
		if input.ID < 1 {
			return result, errors.New("role ID must be positive")
		}
		if _, exists := byID[input.ID]; exists {
			return result, fmt.Errorf("duplicate role ID %d", input.ID)
		}
		input.IsLeaf = input.ChildCount == 0
		input.DescendantIDs = []int64{}
		value := input
		byID[input.ID] = &value
		keys[input.ID] = rucollation.Keys(utf16.Encode([]rune(input.Name)))
		order = append(order, input.ID)
	}
	for _, id := range order {
		v := byID[id]
		pid := int64(0)
		if v.ParentID != nil {
			pid = *v.ParentID
			if _, ok := byID[pid]; !ok {
				return result, fmt.Errorf("unknown parent ID for role %d", id)
			}
		}
		children[pid] = append(children[pid], id)
	}
	for _, ids := range children {
		sort.Slice(ids, func(i, j int) bool {
			left, right := byID[ids[i]], byID[ids[j]]
			if left.SortOrder != right.SortOrder {
				return left.SortOrder < right.SortOrder
			}
			if cmp := rucollation.Compare(keys[left.ID], keys[right.ID]); cmp != 0 {
				return cmp < 0
			}
			return left.ID < right.ID
		})
	}
	visiting := map[int64]bool{}
	visited := map[int64]bool{}
	// Detect cycles across all nodes, including disconnected subgraphs.
	var validate func(int64) error
	validate = func(id int64) error {
		if visiting[id] {
			return fmt.Errorf("cycle in vacancy roles at %d", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, child := range children[id] {
			if err := validate(child); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for _, id := range order {
		if err := validate(id); err != nil {
			return result, err
		}
	}
	var pathFor func(int64) string
	pathFor = func(id int64) string {
		role := byID[id]
		if role.ParentID == nil {
			return role.Name
		}
		return pathFor(*role.ParentID) + " → " + role.Name
	}
	var descendants func(int64) []int64
	descendants = func(id int64) []int64 {
		result := []int64{}
		for _, child := range children[id] {
			result = append(result, child)
			result = append(result, descendants(child)...)
		}
		return result
	}
	for _, id := range order {
		v := byID[id]
		v.Path = pathFor(id)
		v.DescendantIDs = descendants(id)
	}
	var nest func(int64) []Node
	nest = func(id int64) []Node {
		result := make([]Node, 0, len(children[id]))
		for _, child := range children[id] {
			result = append(result, Node{Role: *byID[child], Children: nest(child)})
		}
		return result
	}
	result.Tree = nest(0)
	for _, id := range order {
		v := *byID[id]
		result.Roles = append(result.Roles, v)
		if v.ParentID == nil {
			result.Roots = append(result.Roots, v)
		}
		if v.IsLeaf {
			result.Leaves = append(result.Leaves, v)
		}
	}
	return result, nil
}
