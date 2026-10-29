package vacancies

import (
	"sort"
	"strings"
)

// sqliteNoCasePrefix повторяет ASCII-only NOCASE: UTF8-байты вне A–Z
// сохраняются, сравнение останавливается на NUL. При равных префиксах SQLite
// сравнивает полную длину исходных строк в байтах, включая данные после NUL.
func sqliteNoCasePrefix(text string) string {
	if end := strings.IndexByte(text, 0); end >= 0 {
		text = text[:end]
	}
	key := []byte(text)
	for i, b := range key {
		if b >= 'A' && b <= 'Z' {
			key[i] = b + ('a' - 'A')
		}
	}
	return string(key)
}

// orderedVacancyRows сортирует собственный набор по исходному SQL-порядку.
// SQL NULL и пустая дата различаются до project(), которая отдаёт для обеих null.
// Исходные имена/даты не переписываются, ключ компании вычисляется один раз.
func orderedVacancyRows(input []VacancyState) []Vacancy {
	type entry struct {
		state      VacancyState
		companyKey string
	}
	entries := make([]entry, len(input))
	for i, state := range input {
		entries[i] = entry{state: state, companyKey: sqliteNoCasePrefix(state.CompanyName)}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.companyKey != b.companyKey {
			return a.companyKey < b.companyKey
		}
		if len(a.state.CompanyName) != len(b.state.CompanyName) {
			return len(a.state.CompanyName) < len(b.state.CompanyName)
		}
		if a.state.PublishedSQL.Valid != b.state.PublishedSQL.Valid {
			return a.state.PublishedSQL.Valid
		}
		if a.state.PublishedSQL.Valid && a.state.PublishedSQL.String != b.state.PublishedSQL.String {
			return a.state.PublishedSQL.String > b.state.PublishedSQL.String
		}
		return a.state.ID > b.state.ID
	})
	result := make([]Vacancy, len(entries))
	for i, item := range entries {
		result[i] = item.state.project()
	}
	return result
}
