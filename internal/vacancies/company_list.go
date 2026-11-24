package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"itmap/internal/backup"
)

var ErrCompanyNotFound = errors.New("company not found")

type CompanyListResponse struct {
	CompanyName string            `json:"companyName"`
	Limit       *int              `json:"limit"`
	Vacancies   []CompanyVacancy  `json:"vacancies"`
	Entities    []DirectoryEntity `json:"entities"`
}

func (r Repository) ListForCompany(ctx context.Context, user, company string) (CompanyListResponse, error) {
	var catalog []byte
	if err := r.DB.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&catalog); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CompanyListResponse{}, err
	}
	names, err := backup.StoredCatalogNames(catalog)
	if err != nil {
		return CompanyListResponse{}, err
	}
	if !slices.Contains(names, company) {
		return CompanyListResponse{}, ErrCompanyNotFound
	}
	rows, err := r.CompanyVacancies(ctx, user, company)
	if err != nil {
		return CompanyListResponse{}, err
	}
	related := []string{company}
	for _, row := range rows {
		related = append(related, row.CompanyName, row.RealCompanyName)
	}
	raw, err := backup.PrepareVacancyEntities(catalog, related)
	if err != nil {
		return CompanyListResponse{}, err
	}
	var entities []DirectoryEntity
	if err = json.Unmarshal(raw, &entities); err != nil {
		return CompanyListResponse{}, err
	}
	return CompanyListResponse{CompanyName: company, Vacancies: rows, Entities: entities}, nil
}
