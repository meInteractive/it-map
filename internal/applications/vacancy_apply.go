package applications

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"itmap/internal/vacancies"
)

// VacancyApplyInput is the fixed v316 shortcut payload. The route supplies the
// authoritative vacancy ID; the client only supplies its original company,
// durable UUID and signed generation-bound context.
type VacancyApplyInput struct {
	CompanyName     string `json:"companyName"`
	IdempotencyKey  string `json:"idempotencyKey"`
	CreationContext string `json:"creationContext"`
}

type VacancyApplyResult struct {
	OK                   bool             `json:"ok"`
	Vacancy              vacancies.Detail `json:"vacancy"`
	CreatedApplicationID int64            `json:"createdApplicationId"`
	Replayed             bool             `json:"replayed"`
}

// ApplyToVacancy never trusts the route's vacancy owner from the browser. The
// current owner is rechecked under a FOR UPDATE lock before the receipt is
// claimed. Existing applications can be claimed without changing their status.
func (r Repository) ApplyToVacancy(ctx context.Context, user string, vacancyID int64, raw VacancyApplyInput) (VacancyApplyResult, error) {
	if vacancyID < 1 || strings.TrimSpace(raw.CompanyName) == "" {
		return VacancyApplyResult{}, ErrInvalidInput
	}
	input := CreateInput{
		CompanyName: raw.CompanyName, VacancyID: &vacancyID,
		Status: "Откликнулся", ApplicationSources: []string{"hh"},
		IdempotencyKey: raw.IdempotencyKey, CreationContext: raw.CreationContext,
	}
	result, err := r.createWithReceipt(ctx, user, input, true)
	if err != nil {
		return VacancyApplyResult{}, err
	}
	// A lost response after the receipt commits is safe to retry. Never
	// report success unless the vacancy projection is readable and consistent.
	detail, err := (vacancies.Repository{DB: r.DB}).DetailByID(ctx, user, vacancyID)
	if errors.Is(err, vacancies.ErrNotFound) {
		return VacancyApplyResult{}, ErrShortcutVacancyMissing
	}
	if err != nil {
		return VacancyApplyResult{}, err
	}
	if detail.Vacancy.CompanyName != strings.TrimSpace(raw.CompanyName) {
		return VacancyApplyResult{}, ErrVacancyOwnerChanged
	}
	if detail.Vacancy.ApplicationID == nil || *detail.Vacancy.ApplicationID != result.CreatedApplicationID {
		return VacancyApplyResult{}, ErrIncompleteReceipt
	}
	// The restore generation must remain unchanged throughout response reads.
	generation, err := r.generation(ctx)
	if err != nil {
		return VacancyApplyResult{}, err
	}
	if err := r.verifyContext(user, generation, raw.CreationContext); err != nil {
		return VacancyApplyResult{}, err
	}
	return VacancyApplyResult{OK: true, Vacancy: detail.Vacancy, CreatedApplicationID: result.CreatedApplicationID, Replayed: result.Replayed}, nil
}

// finishCreateForMode is used after the transaction commits. A shortcut only
// needs the created identity; a normal application creation still requires the
// full company response. Channel-owned vacancy shortcuts are valid even when
// the owner is not listed as a company/agency in the public catalog.
func (r Repository) finishCreateForMode(ctx context.Context, user, company, generation string, id int64, replayed, shortcut bool) (CreationResult, error) {
	if !shortcut {
		return r.finishCreate(ctx, user, company, generation, id, replayed)
	}
	actual, err := r.generation(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	if actual != generation {
		return CreationResult{}, ErrCreationContextExpired
	}
	var storedID int64
	err = r.DB.QueryRowContext(ctx, `SELECT id FROM company_applications WHERE id=? AND user_name=?`, id, user).Scan(&storedID)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationResult{}, ErrApplicationDeleted
	}
	if err != nil {
		return CreationResult{}, err
	}
	actual, err = r.generation(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	if actual != generation {
		return CreationResult{}, ErrCreationContextExpired
	}
	return CreationResult{CreatedApplicationID: storedID, Replayed: replayed}, nil
}
