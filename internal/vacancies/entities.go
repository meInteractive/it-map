package vacancies

import (
	"encoding/json"

	"itmap/internal/backup"
)

// DirectoryEntity содержит поля source readEntityDirectory(..., true).
// Четыре пользовательских флага принадлежат только Entity списка вакансий.
type DirectoryEntity struct {
	Name           string `json:"name"`
	Rank           any    `json:"rank"`
	EntityType     string `json:"entityType"`
	Middle         any    `json:"middle"`
	Senior         any    `json:"senior"`
	Lead           any    `json:"lead"`
	Head           any    `json:"head"`
	ApplicationURL any    `json:"applicationUrl"`
}

func vacancyCatalogEntries(data []byte, names []string) ([]map[string]any, error) {
	body, err := backup.PrepareVacancyEntities(data, names)
	if err != nil {
		return nil, err
	}
	var entries []map[string]any
	if err = json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func detailEntities(data []byte, company, realCompany string) ([]DirectoryEntity, error) {
	body, err := backup.PrepareVacancyEntities(data, []string{company, realCompany})
	if err != nil {
		return nil, err
	}
	var entries []DirectoryEntity
	if err = json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
