package backup

import "encoding/json"

// PrepareVacancyEntities повторяет readEntityDirectory(..., true) на принятом
// нормализаторе каталога. Фильтр имён применяется после общих сортировки и рангов.
// Результат содержит только восемь полей справочника, без jobs и личных данных.
func PrepareVacancyEntities(data []byte, names []string) (json.RawMessage, error) {
	root, err := storedReadCatalog(data)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[readNameKey(name)] = true
	}
	result := &jsonValue{kind: '[', items: []*jsonValue{}}
	for _, kind := range []string{"companies", "agencies"} {
		for _, entity := range root.field(kind).items {
			if !wanted[unitsKey(entity.field("name").text)] {
				continue
			}
			item := catalogObject()
			for _, field := range []string{"name", "rank", "entityType", "middle", "senior", "lead", "head", "applicationUrl"} {
				readMember(item, entity, field)
			}
			result.items = append(result.items, item)
		}
	}
	return rawValue(result), nil
}
