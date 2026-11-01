package apifyruns

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"itmap/internal/apifysettings"
)

var ErrStorage = errors.New("журнал Apify недоступен")
var ErrBusy = errors.New("другой запуск Apify ещё выполняется")
var ErrBudget = errors.New("недостаточно подтверждённого бюджета Apify")

type Repository struct {
	DB       *sql.DB
	Settings apifysettings.Repository
}
type Reservation struct {
	ID        string
	State     string
	RunID     string
	DatasetID string
	Existing  bool
}
type account struct {
	ID      string  `json:"accountId"`
	Price   float64 `json:"planPriceUsd"`
	Credits float64 `json:"planCreditsUsd"`
	Limit   float64 `json:"providerLimitUsd"`
	Used    float64 `json:"providerUsedUsd"`
	Start   string  `json:"periodStart"`
	End     string  `json:"periodEnd"`
}

var accountID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func parseAccount(raw []byte, profile string, now time.Time) (account, error) {
	var a account
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return a, ErrBudget
	}
	for _, k := range []string{"accountId", "planPriceUsd", "planCreditsUsd", "providerLimitUsd", "providerUsedUsd", "periodStart", "periodEnd"} {
		if len(fields[k]) == 0 || string(fields[k]) == "null" {
			return a, ErrBudget
		}
	}
	if json.Unmarshal(raw, &a) != nil || !accountID.MatchString(a.ID) {
		return account{}, ErrBudget
	}
	start, e := time.Parse(time.RFC3339Nano, a.Start)
	if e != nil {
		return account{}, ErrBudget
	}
	end, e := time.Parse(time.RFC3339Nano, a.End)
	if e != nil || now.Before(start) || !now.Before(end) {
		return account{}, ErrBudget
	}
	if a.Price < 0 || profile == "free" && a.Price != 0 || profile == "starter" && (a.Price < 19 || a.Credits < 19) {
		return account{}, ErrBudget
	}
	if _, e = CalculateBudget(BudgetInput{Profile: profile, PlanCreditsUSD: a.Credits, ProviderLimitUSD: a.Limit, ProviderUsedUSD: a.Used}); e != nil {
		return account{}, ErrBudget
	}
	return a, nil
}

// Reserve не выполняет сеть. Все операции используют один порядок блокировок:
// поколение, настройки Apify, затем чтение источника и денежного журнала.
func (r Repository) Reserve(ctx context.Context, v apifysettings.OperationVersion, operationKey string) (Reservation, error) {
	if r.DB == nil || r.Settings.DB != r.DB || operationKey == "" || len(operationKey) > 1024 {
		return Reservation{}, ErrStorage
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return Reservation{}, ErrStorage
	}
	defer tx.Rollback()
	var generation string
	if tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation) != nil {
		return Reservation{}, ErrStorage
	}
	if generation != v.Generation {
		return Reservation{}, apifysettings.ErrGeneration
	}
	var revision int64
	if tx.QueryRowContext(ctx, "SELECT revision FROM apify_settings WHERE id=1 FOR UPDATE").Scan(&revision) != nil {
		return Reservation{}, ErrStorage
	}
	if revision != v.SettingsRevision {
		return Reservation{}, apifysettings.ErrChanged
	}
	token, e := r.Settings.ResolveOperationTx(ctx, tx, v)
	if e != nil {
		return Reservation{}, e
	}
	sum := sha256.Sum256([]byte(token))
	hash := hex.EncodeToString(sum[:])
	var out Reservation
	var previousHash, profile string
	e = tx.QueryRowContext(ctx, "SELECT id,state,run_id,dataset_id,key_hash,profile FROM apify_runs WHERE generation=? AND operation_key=?", generation, operationKey).Scan(&out.ID, &out.State, &out.RunID, &out.DatasetID, &previousHash, &profile)
	if e == nil {
		if hash != previousHash || profile != v.Profile {
			return Reservation{}, apifysettings.ErrChanged
		}
		out.Existing = true
		if tx.Commit() != nil {
			return Reservation{}, ErrStorage
		}
		return out, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Reservation{}, ErrStorage
	}
	now := time.Now()
	var busy int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_runs WHERE state IN ('starting','running','uncertain') AND created_at>?", now.UnixMilli()-300000).Scan(&busy) != nil {
		return Reservation{}, ErrStorage
	}
	if busy > 0 {
		return Reservation{}, ErrBusy
	}
	column := "free_account_json"
	if v.Profile == "starter" {
		column = "starter_account_json"
	}
	var raw []byte
	if tx.QueryRowContext(ctx, "SELECT "+column+" FROM apify_settings WHERE id=1").Scan(&raw) != nil {
		return Reservation{}, ErrStorage
	}
	a, e := parseAccount(raw, v.Profile, now)
	if e != nil {
		return Reservation{}, e
	}
	in := BudgetInput{Profile: v.Profile, PlanCreditsUSD: a.Credits, ProviderLimitUSD: a.Limit, ProviderUsedUSD: a.Used}
	usageID, _ := json.Marshal([]string{a.ID, a.Start, a.End})
	e = tx.QueryRowContext(ctx, "SELECT provider_used_usd FROM apify_account_usage WHERE id=?", string(usageID)).Scan(&in.HighWaterUSD)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return Reservation{}, ErrStorage
	}
	if tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(charged_usd),0),COALESCE(SUM(CASE WHEN charged_usd IS NULL THEN reserved_usd ELSE 0 END),0),COUNT(*),COALESCE(SUM(CASE WHEN charged_usd IS NULL THEN 1 ELSE 0 END),0) FROM apify_runs WHERE account_id=? AND period_start=? AND period_end=?", a.ID, a.Start, a.End).Scan(&in.ConfirmedUSD, &in.UnknownReserveUSD, &in.Runs, &in.UnknownRuns) != nil {
		return Reservation{}, ErrStorage
	}
	budget, e := CalculateBudget(in)
	if e != nil {
		return Reservation{}, e
	}
	if budget.RemainingUSD+1e-9 < budget.MinimumReserveUSD {
		return Reservation{}, ErrBudget
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return Reservation{}, ErrStorage
	}
	out = Reservation{ID: hex.EncodeToString(id[:]), State: "starting"}
	_, e = tx.ExecContext(ctx, "INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'starting',?,?)", out.ID, generation, operationKey, v.Profile, a.ID, hash, a.Start, a.End, RunCapUSD, now.UnixMilli(), now.UnixMilli())
	if e != nil {
		return Reservation{}, ErrStorage
	}
	if tx.Commit() != nil {
		return Reservation{}, ErrStorage
	}
	return out, nil
}
