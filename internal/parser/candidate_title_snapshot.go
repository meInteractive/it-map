package parser

import (
	"context"
	"database/sql"
	"errors"
	"itmap/internal/aiproviders"
)

type CandidateTitleInput CandidateStageInput

// ReadCandidateTitleInput собирает только часть снимка планировщика: её нельзя
// использовать для выбора стадий после классификации без проверки остальных зависимостей.
// Владелец задаётся сервером, данные задания служат лишь для проверки scope.
func ReadCandidateTitleInput(ctx context.Context, tx *sql.Tx, job Job, owner string) (CandidateTitleInput, error) {
	if tx == nil {
		return CandidateTitleInput{}, ErrJobInput
	}
	shadow := job
	shadow.Type = "classify_title"
	title, err := (TitleStageStore{Owner: owner}).read(ctx, tx, shadow)
	if err != nil {
		return CandidateTitleInput{}, err
	}
	input := CandidateStageInput{CandidateID: title.CandidateID, TitleRevision: title.Revision, TitleHash: title.Hash, CompanyID: cloneInt(job.CompanyID), AgencyID: cloneInt(job.AgencyID), SourceID: *job.SourceID, SourceSelected: true, ScopeAllowed: true}
	for _, role := range title.Roles {
		r := CandidateStageRole{ID: role.Role.RoleID, PromptVersion: role.Hash, ModelKey: aiproviders.JEVModelID}
		var decision, model string
		err = tx.QueryRowContext(ctx, `SELECT decision,model_id FROM vacancy_classifications WHERE candidate_id=? AND root_role_id=? AND title_hash=? AND title_revision=? AND prompt_version=? AND model_key=? ORDER BY id DESC LIMIT 1 FOR UPDATE`, title.CandidateID, r.ID, title.Hash, title.Revision, role.Hash, r.ModelKey).Scan(&decision, &model)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return CandidateTitleInput{}, ErrSettingsStorage
		}
		if err == nil {
			if (decision != "match" && decision != "reject") || model == "" {
				return CandidateTitleInput{}, ErrSettingsStorage
			}
			r.Decision = &decision
		}
		input.Roles = append(input.Roles, r)
	}
	return CandidateTitleInput(input), nil
}
