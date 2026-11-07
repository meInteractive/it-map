package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// AISpendingDebugRepository читает только журналы подтверждённых и неизвестных
// затрат ИИ; операции из других провайдеров не попадают в числитель.
type AISpendingDebugRepository struct {
	DB  *sql.DB
	Now func() time.Time
}

type aiCostSummary struct {
	ConfirmedUsd         float64 `json:"confirmedUsd"`
	UnknownReservedUsd   float64 `json:"unknownReservedUsd"`
	UpperEstimateUsd     float64 `json:"upperEstimateUsd"`
	UnknownCostOperations int    `json:"unknownCostOperations"`
	UnpricedOperations    int    `json:"unpricedOperations"`
}

func (s *aiCostSummary) add(reserved float64, cost sql.NullFloat64) bool {
	if math.IsNaN(reserved) || math.IsInf(reserved, 0) || reserved < 0 {
		return false
	}
	if cost.Valid {
		if math.IsNaN(cost.Float64) || math.IsInf(cost.Float64, 0) || cost.Float64 < 0 {
			return false
		}
		s.ConfirmedUsd += cost.Float64
	} else if reserved > 0 {
		s.UnknownReservedUsd += reserved
		s.UnknownCostOperations++
	} else {
		s.UnpricedOperations++
	}
	s.UpperEstimateUsd = s.ConfirmedUsd + s.UnknownReservedUsd
	return true
}

type aiCostGroup struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Type     string `json:"type"`
	aiCostSummary
}

func (r AISpendingDebugRepository) ReadAISpending(ctx context.Context) (json.RawMessage, error) {
	if r.DB == nil {
		return nil, ErrDashboardStorage
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	period := SchedulerPeriod(now)
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, ErrDashboardStorage
	}
	defer tx.Rollback()
	// Расходы исторические, поэтому НЕ ограничены текущей generation.
	// Число операций учитывается по уникальному id scheduler_attempts.
	rows, err := tx.QueryContext(ctx,
		"SELECT provider,model,type,started_at,reserved_usd,cost_usd FROM scheduler_attempts WHERE started_at>=? AND provider IN ('openai','openrouter') ORDER BY started_at,id",
		period.Month)
	if err != nil {
		return nil, ErrDashboardStorage
	}
	month := aiCostSummary{}
	day := aiCostSummary{}
	byProvider := map[string]*aiCostSummary{
		"openai": {}, "openrouter": {},
	}
	byGroup := map[string]*aiCostGroup{}
	for rows.Next() {
		var provider, kind string
		var model sql.NullString
		var started int64
		var reserved sql.NullFloat64
		var cost sql.NullFloat64
		if err = rows.Scan(&provider, &model, &kind, &started, &reserved, &cost); err != nil {
			break
		}
		amount := float64(0)
		if reserved.Valid {
			amount = reserved.Float64
		}
		if !month.add(amount, cost) {
			err = ErrDashboardStorage
			break
		}
		if started >= period.Day && !day.add(amount, cost) {
			err = ErrDashboardStorage
			break
		}
		providerTotals := byProvider[provider]
		if providerTotals == nil || !providerTotals.add(amount, cost) {
			err = ErrDashboardStorage
			break
		}
		modelName := ""
		if model.Valid {
			modelName = model.String
		}
		key := provider + "\x00" + modelName + "\x00" + kind
		group := byGroup[key]
		if group == nil {
			group = &aiCostGroup{Provider: provider, Model: modelName, Type: kind}
			byGroup[key] = group
		}
		if !group.aiCostSummary.add(amount, cost) {
			err = ErrDashboardStorage
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, ErrDashboardStorage
	}
	groups := make([]aiCostGroup, 0, len(byGroup))
	for _, group := range byGroup {
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].UpperEstimateUsd != groups[j].UpperEstimateUsd {
			return groups[i].UpperEstimateUsd > groups[j].UpperEstimateUsd
		}
		if groups[i].Provider != groups[j].Provider {
			return groups[i].Provider < groups[j].Provider
		}
		if groups[i].Model != groups[j].Model {
			return groups[i].Model < groups[j].Model
		}
		return groups[i].Type < groups[j].Type
	})
	var total int64
	if err = tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM scheduler_attempts WHERE provider IN ('openai','openrouter')").Scan(&total); err != nil {
		return nil, ErrDashboardStorage
	}
	// Последние десять ИИ-попыток по всей истории: не окно из 50 общих
	// попыток dashboard. Не читаем message, result_json, prompt или payload.
	recent, err := tx.QueryContext(ctx,
		"SELECT id,job_id,run_id,type,source_id,status,reason_code,provider,model,started_at,finished_at,reserved_usd,cost_usd,credits FROM scheduler_attempts WHERE provider IN ('openai','openrouter') ORDER BY started_at DESC,BINARY id DESC LIMIT 10")
	if err != nil {
		return nil, ErrDashboardStorage
	}
	items := make([]map[string]any, 0, 10)
	for recent.Next() {
		var id, jobID, kind, status, provider string
		var runID, sourceID, reason, model sql.NullString
		var started int64
		var finished sql.NullInt64
		var reserved sql.NullFloat64
		var cost, credits sql.NullFloat64
		if err = recent.Scan(&id, &jobID, &runID, &kind, &sourceID, &status, &reason, &provider, &model,
			&started, &finished, &reserved, &cost, &credits); err != nil {
			break
		}
		var finish, price, credit, source, run, reasonCode, modelName any
		if finished.Valid { finish = finished.Int64 }
		if cost.Valid { price = cost.Float64 }
		if credits.Valid { credit = credits.Float64 }
		if sourceID.Valid { source = sourceID.String }
		if runID.Valid { run = runID.String }
		if reason.Valid { reasonCode = reason.String }
		if model.Valid { modelName = model.String }
		var reserve any
		if reserved.Valid { reserve = reserved.Float64 }
		items = append(items, map[string]any{
			"attemptId": id, "jobId": jobID, "runId": run, "type": kind,
			"sourceId": source, "status": status, "reasonCode": reasonCode,
			"provider": provider, "model": modelName, "startedAt": started,
			"finishedAt": finish, "reservedUsd": reserve, "costUsd": price,
			"credits": credit,
		})
	}
	if err == nil {
		err = recent.Err()
	}
	recent.Close()
	if err != nil {
		return nil, ErrDashboardStorage
	}
	result := map[string]any{
		"checkedAt": now.UnixMilli(), "dayStart": period.Day,
		"monthStart": period.Month, "timezone": "Europe/Moscow",
		"day": day, "month": month,
		"providers": byProvider, "models": groups,
		"latest": items, "total": total,
		// Проверка дублирующих платных вызовов — отдельная GO-434.
		"duplicateAICallsAvailable": false,
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, ErrDashboardStorage
	}
	if err := tx.Commit(); err != nil {
		return nil, ErrDashboardStorage
	}
	return payload, nil
}
