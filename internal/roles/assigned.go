package roles

import (
	"sort"
	"unicode/utf16"

	"itmap/internal/rucollation"
)

// OrderAssigned возвращает собственный срез ролей в русском порядке их путей.
// При равных путях сохраняется входной порядок SQL (role_id), как в исходнике.
// Ключи вычисляются один раз; имена, пути и метаданные ролей не изменяются.
func OrderAssigned(input []Role) []Role {
	type entry struct {
		role Role
		key  rucollation.Key
	}
	entries := make([]entry, len(input))
	for i, role := range input {
		entries[i] = entry{role: role, key: rucollation.Keys(utf16.Encode([]rune(role.Path)))}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return rucollation.Compare(entries[i].key, entries[j].key) < 0
	})
	result := make([]Role, len(input))
	for i, e := range entries {
		result[i] = e.role
	}
	return result
}
