package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"time"
)

// GO-434: a bounded read-only diagnostic, NOT proof of duplicate provider charges.
// All provider/account/job identifiers stay inside the repository and never enter JSON.
var ErrExpenseRepeatsInput = errors.New("invalid repeat diagnostic period")
var ErrExpenseRepeatsStorage = errors.New("repeat diagnostic storage unavailable")

type ExpenseRepeatsRepository struct {
	DB  *sql.DB
	Now func() time.Time
}

type repeatApifySummary struct {
	ActorOperations          int64   `json:"actorOperations"`
	LocalReceipts            int64   `json:"localReceipts"`
	RepeatedReceiptGroups    int64   `json:"repeatedReceiptGroups"`
	ExtraLocalReceipts       int64   `json:"extraLocalReceipts"`
	ConflictingActorGroups   int64   `json:"conflictingActorGroups"`
	SuspectedLaunchGroups    int64   `json:"suspectedLaunchGroups"`
	ActorsInSuspectedGroups  int64   `json:"actorsInSuspectedGroups"`
	MissingActorIdentity     int64   `json:"missingActorIdentity"`
	UnknownStartReserveUSD   float64 `json:"unknownStartReserveUsd"`
}

type repeatAIStage struct {
	Type                 string  `json:"type"`
	RepeatGroups         int64   `json:"repeatGroups"`
	ExtraAttempts        int64   `json:"extraAttempts"`
	KnownCostAttempts    int64   `json:"knownCostAttempts"`
	KnownLocalCostUSD    float64 `json:"knownLocalCostUsd"`
	UnknownCostAttempts  int64   `json:"unknownCostAttempts"`
	UnknownReserveUSD    float64 `json:"unknownReserveUsd"`
	UnpricedAttempts     int64   `json:"unpricedAttempts"`
}

type repeatAISummary struct {
	ObservedAttempts int64           `json:"observedAttempts"`
	RepeatGroups     int64           `json:"repeatGroups"`
	ExtraAttempts    int64           `json:"extraAttempts"`
	Stages           []repeatAIStage `json:"stages"`
}

type expenseRepeatsReport struct {
	CheckedAt int64              `json:"checkedAt"`
	Timezone  string             `json:"timezone"`
	PeriodDays int               `json:"periodDays"`
	From      string             `json:"from"`
	To        string             `json:"to"`
	Apify     repeatApifySummary `json:"apify"`
	AI        repeatAISummary    `json:"ai"`
	ProviderRepeatVerified bool  `json:"providerRepeatVerified"`
	WindowLimited bool           `json:"windowLimited"`
}

type repeatActorEvidence struct {
	Account      string
	OperationKey string
	Generation   int64
	RunID        string
	ReservedUSD  float64
	ChargedUSD   sql.NullFloat64
}

type repeatAIEvidence struct {
	JobID       string
	Type        string
	Provider    string
	ReservedUSD float64
	CostUSD     sql.NullFloat64
}

func repeatValidAmount(v float64) bool {
	return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

func repeatKey(parts ...string) string {
	key := ""
	for _, part := range parts {
		key += strconv.Itoa(len(part)) + ":" + part
	}
	return key
}

type repeatActorGroup struct {
	Rows         int64
	Cost         float64
	HasCost      bool
	Conflict     bool
}

func summarizeRepeatApify(items []repeatActorEvidence) (repeatApifySummary, error) {
	out := repeatApifySummary{}
	actors := map[string]*repeatActorGroup{}
	launches := map[string]map[string]struct{}{}
	for _, item := range items {
		if !repeatValidAmount(item.ReservedUSD) || (item.ChargedUSD.Valid && !repeatValidAmount(item.ChargedUSD.Float64)) {
			return repeatApifySummary{}, ErrExpenseRepeatsStorage
		}
		out.LocalReceipts++
		if item.RunID == "" || item.Account == "" {
			out.MissingActorIdentity++
			if !item.ChargedUSD.Valid {
				out.UnknownStartReserveUSD += item.ReservedUSD
			}
			continue
		}
		actorKey := repeatKey(item.Account, item.RunID)
		group := actors[actorKey]
		if group == nil {
			group = &repeatActorGroup{}
			actors[actorKey] = group
		}
		group.Rows++
		if item.ChargedUSD.Valid {
			if group.HasCost && group.Cost != item.ChargedUSD.Float64 {
				group.Conflict = true
			}
			group.HasCost = true
			group.Cost = item.ChargedUSD.Float64
		}
		// An operation_key is a scheduler idempotency scope, not an invoice.
		// Only DISTINCT provider Actor IDs within the SAME scope are suspicious.
		if item.OperationKey != "" {
			key := repeatKey(item.Account, strconv.FormatInt(item.Generation, 10), item.OperationKey)
			if launches[key] == nil {
				launches[key] = map[string]struct{}{}
			}
			launches[key][actorKey] = struct{}{}
		}
	}
	out.ActorOperations = int64(len(actors))
	for _, group := range actors {
		if group.Rows > 1 {
			out.RepeatedReceiptGroups++
			out.ExtraLocalReceipts += group.Rows - 1
		}
		if group.Conflict {
			out.ConflictingActorGroups++
		}
	}
	for _, runs := range launches {
		if len(runs) > 1 {
			out.SuspectedLaunchGroups++
			out.ActorsInSuspectedGroups += int64(len(runs))
		}
	}
	if !repeatValidAmount(out.UnknownStartReserveUSD) {
		return repeatApifySummary{}, ErrExpenseRepeatsStorage
	}
	return out, nil
}

type repeatAIGroup struct {
	Type     string
	Attempts []repeatAIEvidence
}

func repeatStageType(value string) string {
	switch value {
	case "classify_title", "extract_vacancy", "assign_roles", "fetch_full_text", "score_relevance":
		return value
	default:
		return "other"
	}
}

func summarizeRepeatAI(items []repeatAIEvidence) (repeatAISummary, error) {
	out := repeatAISummary{Stages: []repeatAIStage{}}
	groups := map[string]*repeatAIGroup{}
	for _, item := range items {
		if !repeatValidAmount(item.ReservedUSD) || (item.CostUSD.Valid && !repeatValidAmount(item.CostUSD.Float64)) {
			return repeatAISummary{}, ErrExpenseRepeatsStorage
		}
		out.ObservedAttempts++
		// Missing job identity cannot prove two attempts belong to one job.
		if item.JobID == "" {
			continue
		}
		key := repeatKey(item.JobID, item.Type, item.Provider)
		group := groups[key]
		if group == nil {
			group = &repeatAIGroup{Type: repeatStageType(item.Type)}
			groups[key] = group
		}
		group.Attempts = append(group.Attempts, item)
	}
	byStage := map[string]*repeatAIStage{}
	for _, group := range groups {
		if len(group.Attempts) < 2 {
			continue
		}
		out.RepeatGroups++
		out.ExtraAttempts += int64(len(group.Attempts) - 1)
		stage := byStage[group.Type]
		if stage == nil {
			stage = &repeatAIStage{Type: group.Type}
			byStage[group.Type] = stage
		}
		stage.RepeatGroups++
		stage.ExtraAttempts += int64(len(group.Attempts) - 1)
		for _, attempt := range group.Attempts {
			if attempt.CostUSD.Valid {
				stage.KnownCostAttempts++
				stage.KnownLocalCostUSD += attempt.CostUSD.Float64
			} else if attempt.ReservedUSD > 0 {
				stage.UnknownCostAttempts++
				stage.UnknownReserveUSD += attempt.ReservedUSD
			} else {
				stage.UnpricedAttempts++
			}
		}
	}
	for _, stage := range byStage {
		if !repeatValidAmount(stage.KnownLocalCostUSD) || !repeatValidAmount(stage.UnknownReserveUSD) {
			return repeatAISummary{}, ErrExpenseRepeatsStorage
		}
		out.Stages = append(out.Stages, *stage)
	}
	sort.Slice(out.Stages, func(i, j int) bool { return out.Stages[i].Type < out.Stages[j].Type })
	return out, nil
}

func (r ExpenseRepeatsRepository) ReadExpenseRepeats(ctx context.Context, days int) (json.RawMessage, error) {
	if days != 7 && days != 30 {
		return nil, ErrExpenseRepeatsInput
	}
	if r.DB == nil {
		return nil, ErrExpenseRepeatsStorage
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	window, daily := unitCostWindowFor(now, days)
	report := expenseRepeatsReport{
		CheckedAt: now.UnixMilli(),
		Timezone: "Europe/Moscow",
		PeriodDays: days,
		From: daily[0].Day,
		To: daily[len(daily)-1].Day,
		AI: repeatAISummary{Stages: []repeatAIStage{}},
		ProviderRepeatVerified: false,
		WindowLimited: true,
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	defer tx.Rollback()
	// Only local receipts: no provider API, no raw content or applicant records.
	actors, err := tx.QueryContext(ctx, `SELECT account_id, operation_key, generation, run_id, reserved_usd, charged_usd
		FROM apify_runs WHERE created_at>=? AND created_at<? ORDER BY created_at, BINARY id`, window.start, window.end)
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	actorEvidence := []repeatActorEvidence{}
	for actors.Next() {
		var item repeatActorEvidence
		if err = actors.Scan(&item.Account, &item.OperationKey, &item.Generation, &item.RunID, &item.ReservedUSD, &item.ChargedUSD); err != nil {
			break
		}
		actorEvidence = append(actorEvidence, item)
	}
	if err == nil {
		err = actors.Err()
	}
	actors.Close()
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	report.Apify, err = summarizeRepeatApify(actorEvidence)
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	attempts, err := tx.QueryContext(ctx, `SELECT job_id, type, provider, reserved_usd, cost_usd
		FROM scheduler_attempts WHERE started_at>=? AND started_at<?
		AND provider IN ('openai','openrouter') ORDER BY started_at, BINARY id`, window.start, window.end)
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	aiEvidence := []repeatAIEvidence{}
	for attempts.Next() {
		var item repeatAIEvidence
		if err = attempts.Scan(&item.JobID, &item.Type, &item.Provider, &item.ReservedUSD, &item.CostUSD); err != nil {
			break
		}
		aiEvidence = append(aiEvidence, item)
	}
	if err == nil {
		err = attempts.Err()
	}
	attempts.Close()
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	report.AI, err = summarizeRepeatAI(aiEvidence)
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	if err = tx.Commit(); err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	result, err := json.Marshal(report)
	if err != nil {
		return nil, ErrExpenseRepeatsStorage
	}
	return result, nil
}
