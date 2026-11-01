package parser

import (
	"context"
	"database/sql"
	"reflect"
)

// validateImportStageJob сравнивает задание с повторно прочитанными данными.
// Разрешение imported возможно только после отдельной проверки receipt в TX.
func validateImportStageJob(job Job, input CandidateStageInput, receiptVerified bool) error {
	if job.Type != "import_vacancy" || job.CandidateID == nil || job.SourceID == nil || *job.CandidateID != input.CandidateID || *job.SourceID != input.SourceID || !reflect.DeepEqual(job.CompanyID, input.CompanyID) || !reflect.DeepEqual(job.AgencyID, input.AgencyID) || (input.Imported && !receiptVerified) {
		return ErrCandidateChanged
	}
	input.Imported = false
	plan, err := PlanCandidateStage(input)
	if err != nil {
		return err
	}
	if plan == nil || plan.Type != job.Type || stageHash(plan.Key) != job.DedupeKey {
		return ErrCandidateChanged
	}
	return nil
}

// readImportStageSnapshot вызывается внутри owned TX; внешние данные job.payload
// не участвуют в выборе роли, извлечения или проверке версии задания.
func readImportStageSnapshot(ctx context.Context, tx *sql.Tx, job Job, owner string) (leafSnapshot, *leafCache, error) {
	input, err := ReadCandidateLeafInput(ctx, tx, job, owner)
	if err != nil {
		return leafSnapshot{}, nil, err
	}
	if err = validateImportStageJob(job, CandidateStageInput(input), false); err != nil {
		return leafSnapshot{}, nil, err
	}
	shadow := job
	shadow.Type = "assign_roles"
	snapshot, err := (LeafStageStore{Owner: owner}).read(ctx, tx, shadow)
	if err != nil {
		return leafSnapshot{}, nil, err
	}
	leaf, err := readLeafCache(ctx, tx, snapshot)
	if err != nil {
		return leafSnapshot{}, nil, err
	}
	if leaf == nil || snapshot.ExtractCache == nil || snapshot.ExtractCache.ID != input.ExtractionID || leaf.ID != input.Leaf.ID || snapshot.InputHash != input.LeafInputHash {
		return leafSnapshot{}, nil, ErrCandidateChanged
	}
	return snapshot, leaf, nil
}
