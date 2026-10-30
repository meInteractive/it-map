package parser

import (
	"context"
	"database/sql"
)

type CandidateLeafInput CandidateStageInput

// ReadCandidateLeafInput читает актуальные роли и их результат в той же
// транзакции. Разрешение импортированной вакансии остаётся отдельной задачей.
func ReadCandidateLeafInput(ctx context.Context, tx *sql.Tx, job Job, owner string) (CandidateLeafInput, error) {
	extracted, err := ReadCandidateExtractionInput(ctx, tx, job, owner)
	if err != nil {
		return CandidateLeafInput{}, err
	}
	input := CandidateStageInput(extracted)
	if input.ExtractionID == "" {
		return CandidateLeafInput(input), nil
	}
	shadow := job
	shadow.Type = "assign_roles"
	leaf, err := (LeafStageStore{Owner: owner}).read(ctx, tx, shadow)
	if err != nil {
		return CandidateLeafInput{}, err
	}
	if leaf.ExtractCache.ID != input.ExtractionID {
		return CandidateLeafInput{}, ErrCandidateChanged
	}
	input.LeafInputHash = leaf.InputHash
	cache, err := readLeafCache(ctx, tx, leaf)
	if err != nil {
		return CandidateLeafInput{}, err
	}
	if cache != nil {
		input.Leaf = &CandidateStageLeaf{ID: cache.ID, InputHash: leaf.InputHash, RoleIDs: cache.IDs}
	}
	var imported sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT main_vacancy_id FROM vacancy_candidates WHERE id=? FOR UPDATE", input.CandidateID).Scan(&imported); err != nil {
		return CandidateLeafInput{}, ErrSettingsStorage
	}
	input.Imported = imported.Valid && imported.Int64 != 0
	return CandidateLeafInput(input), nil
}
