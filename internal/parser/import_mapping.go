package parser

import (
	"math"
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
		n, ok := data[key].(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 9007199254740991 || math.Trunc(n) != n {
			return nil, ErrSnapshotInvalid
		}
		v := int64(n)
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
