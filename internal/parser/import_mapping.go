package parser

import (
	"encoding/json"
	"strings"

	"itmap/internal/vacancies"
)

// mapImportExtraction переносит только поддерживаемые поля основной карточки.
// Денежная вилка переносится лишь для месячной зарплаты в рублях.
func mapImportExtraction(company, sourceURL, fetchedAt string, roleIDs []int64, data map[string]any) (vacancies.Input, error) {
	stringField := func(key string) string { v, _ := data[key].(string); return v }
	numberField := func(key string) (*int64, error) {
		if data[key] == nil {
			return nil, nil
		}
		n, ok := data[key].(json.Number)
		if !ok {
			return nil, ErrSnapshotInvalid
		}
		v, err := n.Int64()
		if err != nil {
			return nil, ErrSnapshotInvalid
		}
		return &v, nil
	}
	if len(fetchedAt) < 10 || len(roleIDs) == 0 {
		return vacancies.Input{}, ErrSnapshotInvalid
	}
	level := ""
	for _, v := range []string{"Senior", "Middle", "Lead", "Head"} {
		if strings.EqualFold(strings.TrimSpace(stringField("level")), v) {
			level = v
			break
		}
	}
	in := vacancies.Input{CompanyName: company, EntityType: "company", URL: sourceURL, Title: stringField("title"), DescriptionMD: stringField("description_md"), Level: level, PublishedAt: stringField("published_at"), LoadedAt: fetchedAt[:10], RoleIDs: roleIDs}
	if stringField("salary_currency") == "RUB" && stringField("salary_period") == "month" {
		var err error
		if in.SalaryFrom, err = numberField("salary_from"); err != nil {
			return vacancies.Input{}, err
		}
		if in.SalaryTo, err = numberField("salary_to"); err != nil {
			return vacancies.Input{}, err
		}
	}
	return vacancies.NormalizeInput(in, true)
}
