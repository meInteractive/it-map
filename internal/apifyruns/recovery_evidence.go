package apifyruns

import "math"

// RecoveryEvidence описывает только наблюдаемую запись apify_runs, без права
// на POST, settlement или освобождение денежного резерва.
type RecoveryEvidence string

const (
	RecoveryUnknown              RecoveryEvidence = "unknown"
	RecoveryReconcile            RecoveryEvidence = "reconcile"
	RecoveryTerminalCostRecorded RecoveryEvidence = "terminal_cost_recorded"
)

// ClassifyRecoveryEvidence не связывает запись с worker attempt и не проверяет
// её происхождение. Вызывающий обязан читать журнал под необходимыми locks и
// отдельно доказать связь, provider и транзакционные fencing-инварианты.
// Строка без run ID никогда не превращается в разрешение повторной отправки.
func ClassifyRecoveryEvidence(row Record) (RecoveryEvidence, error) {
	if row.ID == "" || !accountID.MatchString(row.AccountID) {
		return "", ErrIdentity
	}
	if row.ChargedUSD != nil && (math.IsNaN(*row.ChargedUSD) || math.IsInf(*row.ChargedUSD, 0) || *row.ChargedUSD < 0) {
		return "", ErrIdentity
	}
	if row.RunID == "" {
		if row.DatasetID != "" || row.ChargedUSD != nil || (row.State != "starting" && row.State != "uncertain") {
			return "", ErrIdentity
		}
		return RecoveryUnknown, nil
	}
	if !accountID.MatchString(row.RunID) || !accountID.MatchString(row.DatasetID) {
		return "", ErrIdentity
	}
	switch row.State {
	case "uncertain", "running":
		if row.ChargedUSD != nil {
			return "", ErrIdentity
		}
		return RecoveryReconcile, nil
	case "succeeded", "failed":
		if row.ChargedUSD == nil {
			return RecoveryReconcile, nil
		}
		return RecoveryTerminalCostRecorded, nil
	default:
		return "", ErrIdentity
	}
}
