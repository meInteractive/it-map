package parser

import (
	"context"
	"database/sql"
	"errors"
)

type CandidateFullTextInput CandidateStageInput

// ReadCandidateFullTextInput повторно проверяет scope и классификацию, затем
// читает полный текст и его свежесть в той же транзакции. Это частичный снимок:
// стадии extraction и roles требуют дополнительных проверенных зависимостей.
func ReadCandidateFullTextInput(ctx context.Context, tx *sql.Tx, job Job, owner string) (CandidateFullTextInput, error) {
	title, err := ReadCandidateTitleInput(ctx, tx, job, owner)
	if err != nil {
		return CandidateFullTextInput{}, err
	}
	input := CandidateStageInput(title)
	matched := false
	for _, role := range input.Roles {
		if role.Decision == nil {
			return CandidateFullTextInput(input), nil
		}
		matched = matched || *role.Decision == "match"
	}
	if !matched {
		return CandidateFullTextInput(input), nil
	}
	var url, updated sql.NullString
	if err = tx.QueryRowContext(ctx, "SELECT vacancy_url,title_updated_at FROM vacancy_candidates WHERE id=? FOR UPDATE", input.CandidateID).Scan(&url, &updated); err != nil {
		return CandidateFullTextInput{}, ErrSettingsStorage
	}
	if updated.Valid {
		input.TitleUpdatedAt = updated.String
	}
	if !url.Valid || url.String == "" {
		return CandidateFullTextInput(input), nil
	}
	snapshot, err := ReadRawSnapshot(ctx, tx, input.CandidateID, url.String, input.SourceID)
	if errors.Is(err, ErrSnapshotMissing) {
		return CandidateFullTextInput(input), nil
	}
	if err != nil {
		return CandidateFullTextInput{}, err
	}
	input.Snapshot = &CandidateStageSnapshot{ID: snapshot.ID, FetchedAt: snapshot.FetchedAt}
	var last int64
	err = tx.QueryRowContext(ctx, "SELECT started_at FROM vacancy_content_fetches WHERE candidate_id=? AND status='success' ORDER BY started_at DESC LIMIT 1 FOR UPDATE", input.CandidateID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CandidateFullTextInput{}, ErrSettingsStorage
	}
	if err == nil {
		input.LastSuccessfulFetch = &last
	}
	return CandidateFullTextInput(input), nil
}
