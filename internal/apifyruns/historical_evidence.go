package apifyruns

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"itmap/internal/apifysettings"
)

const maxHistoricalEvidence = 1024

var ErrHistoricalEvidenceLimit = errors.New("historical paid evidence exceeds diagnostic limit")
var ErrHistoricalEvidenceIncompatible = errors.New("historical paid evidence incompatible")

// HistoricalEvidence — только агрегат строк, которые сейчас присутствуют в БД.
// Категория с записанной стоимостью не подтверждает settlement или полноту
// истории, а нулевые счётчики не разрешают запуск нового worker.
type HistoricalEvidence struct {
	Unknown              int
	Reconcile            int
	TerminalCostRecorded int
}

// InspectHistoricalEvidence кратковременно удерживает generation lock и читает
// записи apify_runs других generation. Участвующие операции Apify блокируют ту
// же generation до записи. Снимок ограничен по размеру; неподдерживаемая строка
// или превышение лимита приводят к отказу без частичного результата. Функция не
// читает архивы и scheduler ledger, не доказывает полноту evidence после restore,
// не освобождает резервы и не разрешает POST. В runtime она не подключена.
func (r Repository) InspectHistoricalEvidence(ctx context.Context, expectedGeneration string) (summary HistoricalEvidence, outcome error) {
	if ctx == nil || r.DB == nil || !jobGenerationPattern.MatchString(expectedGeneration) {
		return HistoricalEvidence{}, ErrStorage
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return HistoricalEvidence{}, historicalStorageError(ctx)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			summary = HistoricalEvidence{}
			outcome = historicalStorageError(ctx)
		}
	}()
	return InspectHistoricalEvidenceTx(ctx, tx, expectedGeneration)
}

// InspectHistoricalEvidenceTx удерживает generation lock до конца caller-owned
// транзакции. Caller ограничивает её время и обязан откатить при ошибке.
// Это read-only snapshot без разрешения на платные действия.
func InspectHistoricalEvidenceTx(ctx context.Context, tx *sql.Tx, expectedGeneration string) (summary HistoricalEvidence, outcome error) {
	if ctx == nil || tx == nil || !jobGenerationPattern.MatchString(expectedGeneration) {
		return HistoricalEvidence{}, ErrStorage
	}
	var current string
	if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&current); err != nil {
		return HistoricalEvidence{}, historicalStorageError(ctx)
	}
	if current != expectedGeneration {
		return HistoricalEvidence{}, apifysettings.ErrGeneration
	}
	rows, err := tx.QueryContext(ctx, "SELECT id,generation,state,run_id,dataset_id,account_id,charged_usd FROM apify_runs WHERE generation<>? ORDER BY id LIMIT 1025 FOR UPDATE", current)
	if err != nil {
		return HistoricalEvidence{}, historicalStorageError(ctx)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > maxHistoricalEvidence {
			return HistoricalEvidence{}, ErrHistoricalEvidenceLimit
		}
		var row Record
		var generation string
		var charged sql.NullFloat64
		if err = rows.Scan(&row.ID, &generation, &row.State, &row.RunID, &row.DatasetID, &row.AccountID, &charged); err != nil {
			return HistoricalEvidence{}, historicalStorageError(ctx)
		}
		if !jobGenerationPattern.MatchString(generation) {
			return HistoricalEvidence{}, ErrHistoricalEvidenceIncompatible
		}
		if charged.Valid {
			row.ChargedUSD = &charged.Float64
		}
		category, err := ClassifyRecoveryEvidence(row)
		if err != nil {
			return HistoricalEvidence{}, ErrHistoricalEvidenceIncompatible
		}
		switch category {
		case RecoveryUnknown:
			summary.Unknown++
		case RecoveryReconcile:
			summary.Reconcile++
		case RecoveryTerminalCostRecorded:
			summary.TerminalCostRecorded++
		default:
			return HistoricalEvidence{}, ErrHistoricalEvidenceIncompatible
		}
	}
	if rows.Err() != nil {
		return HistoricalEvidence{}, historicalStorageError(ctx)
	}
	if err = rows.Close(); err != nil {
		return HistoricalEvidence{}, historicalStorageError(ctx)
	}
	if err = ctx.Err(); err != nil {
		return HistoricalEvidence{}, err
	}
	return summary, nil
}

func historicalStorageError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrStorage
}
