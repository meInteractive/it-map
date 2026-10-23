package applications

import (
	"context"
	"errors"
	"strings"

	"itmap/internal/crm"
	"itmap/internal/vacancies"
)

// CompanyResponse is the v316 GET /api/applications?company=... result and
// the successful PUT/DELETE /api/applications/{id} response. Never replace it
// with a partial {ok:true} or single record: the existing React UI depends
// on records and full vacancy data.
type CompanyResponse struct {
	CompanyName string                     `json:"companyName"`
	Favorite    bool                       `json:"favorite"`
	Hidden      bool                       `json:"hidden"`
	Limit       int                        `json:"limit"`
	Records     []Record                   `json:"records"`
	Vacancies   []vacancies.CompanyVacancy `json:"vacancies"`
}

var ErrCompanyNotFound = crm.ErrCompanyNotFound

// CompanyApplications validates catalog membership, then composes the original
// full company response using existing read-only repositories.
func (r Repository) CompanyApplications(ctx context.Context, user, company string) (CompanyResponse, error) {
	if strings.TrimSpace(company) == "" {
		return CompanyResponse{}, ErrCompanyNotFound
	}
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return CompanyResponse{}, errors.New("company applications unavailable")
	}
	companyState, err := (crm.Repository{DB: r.DB}).Company(ctx, user, company)
	if err != nil {
		return CompanyResponse{}, err
	}
	records, err := r.List(ctx, user, company, nil)
	if err != nil {
		return CompanyResponse{}, err
	}
	companyVacancies, err := (vacancies.Repository{DB: r.DB}).CompanyVacancies(ctx, user, company)
	if err != nil {
		return CompanyResponse{}, err
	}
	return CompanyResponse{CompanyName: company, Favorite: companyState.Favorite, Hidden: companyState.Hidden, Limit: 10, Records: records, Vacancies: companyVacancies}, nil
}
