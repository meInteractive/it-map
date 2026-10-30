package parser

import (
	"context"
	"database/sql"
)

// SQLCandidateHandoffPlanner вычисляет следующую стадию по проверенному
// серверному снимку в принадлежащей текущему заданию транзакции.
type SQLCandidateHandoffPlanner struct{ Owner string }

func (p SQLCandidateHandoffPlanner) Next(ctx context.Context, tx *sql.Tx, job Job) (*CandidateStagePlan, error) {
	input, err := ReadCandidateLeafInput(ctx, tx, job, p.Owner)
	if err != nil {
		return nil, err
	}
	return PlanCandidateStage(CandidateStageInput(input))
}
