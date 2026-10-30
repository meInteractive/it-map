package parser

import (
	"context"
	"database/sql"
)

type CandidateExtractionInput CandidateStageInput

// ReadCandidateExtractionInput не выполняет запросы к ИИ и не планирует стадии
// после extraction без отдельной проверки дерева ролей и результатов.
func ReadCandidateExtractionInput(ctx context.Context, tx *sql.Tx, job Job, owner string) (CandidateExtractionInput, error) {
	full, err := ReadCandidateFullTextInput(ctx, tx, job, owner)
	if err != nil {
		return CandidateExtractionInput{}, err
	}
	input := CandidateStageInput(full)
	matched := false
	for _, role := range input.Roles {
		if role.Decision == nil {
			return CandidateExtractionInput(input), nil
		}
		matched = matched || *role.Decision == "match"
	}
	if !matched || input.Snapshot == nil {
		return CandidateExtractionInput(input), nil
	}
	refresh, err := candidateTextRefresh(input)
	if err != nil {
		return CandidateExtractionInput{}, err
	}
	if refresh {
		return CandidateExtractionInput(input), nil
	}
	shadow := job
	shadow.Type = "extract_vacancy"
	extraction, err := (ExtractionStageStore{Owner: owner}).read(ctx, tx, shadow)
	if err != nil {
		return CandidateExtractionInput{}, err
	}
	if extraction.Snapshot.ID != input.Snapshot.ID {
		return CandidateExtractionInput{}, ErrCandidateChanged
	}
	input.ExtractionConfigHash, input.ExtractionPromptVersion = extraction.ConfigHash, extraction.PromptHash
	cache, err := readExtractionCache(ctx, tx, extraction)
	if err != nil {
		return CandidateExtractionInput{}, err
	}
	if cache != nil {
		input.ExtractionID = cache.ID
	}
	return CandidateExtractionInput(input), nil
}
