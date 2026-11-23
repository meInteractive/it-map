package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"itmap/internal/auth"
	"itmap/internal/integrations/firecrawl"
	"itmap/internal/legacycrypto"
)

var errFirecrawlAccountChanged = errors.New("Firecrawl settings changed")
var errFirecrawlAccountInput = errors.New("invalid Firecrawl account input")

const firecrawlDate = "2006-01-02T15:04:05.000Z"

type firecrawlAccount struct {
	Plan      int64  `json:"planCredits"`
	Remaining int64  `json:"remainingCredits"`
	Start     string `json:"periodStart"`
	End       string `json:"periodEnd"`
	Checked   int64  `json:"checkedAt"`
}

type firecrawlAccountSettings struct {
	mode, selected, generation, key, hash, usage, status string
	revision                                             int64
	enabled                                              bool
}

type firecrawlAccountService struct {
	db        *sql.DB
	codec     *legacycrypto.Codec
	transport http.RoundTripper
}

func (s firecrawlAccountService) read(ctx context.Context, mode string) (firecrawlAccountSettings, error) {
	var v firecrawlAccountSettings
	if s.db == nil {
		return v, sql.ErrConnDone
	}
	err := s.db.QueryRowContext(ctx, `SELECT f.mode, f.enabled=1,f.revision,g.generation,
 CASE WHEN COALESCE(NULLIF(?,''),f.mode)='hobby' THEN hobby_key_encrypted ELSE key_encrypted END,
 CASE WHEN COALESCE(NULLIF(?,''),f.mode)='hobby' THEN hobby_key_hash ELSE free_key_hash END,
 CASE WHEN COALESCE(NULLIF(?,''),f.mode)='hobby' THEN hobby_usage_json ELSE free_usage_json END,
 CASE WHEN COALESCE(NULLIF(?,''),f.mode)='hobby' THEN hobby_connection_status ELSE connection_status END
 FROM firecrawl_settings f JOIN app_runtime_generation g ON g.id=1 WHERE f.id=1`, mode, mode, mode, mode).
		Scan(&v.selected, &v.enabled, &v.revision, &v.generation, &v.key, &v.hash, &v.usage, &v.status)
	v.mode = mode
	if mode == "" {
		v.mode = v.selected
	}
	return v, err
}

func parseFirecrawlAccount(u firecrawl.CreditUsage, now time.Time) (firecrawlAccount, error) {
	start, a := time.Parse(time.RFC3339Nano, u.PeriodStart)
	end, b := time.Parse(time.RFC3339Nano, u.PeriodEnd)
	if a != nil || b != nil || start.After(now) || !end.After(now) || end.Sub(start) < 24*time.Hour || end.Sub(start) > 35*24*time.Hour ||
		u.PlanCredits == nil || u.RemainingCredits == nil || *u.PlanCredits < 1 || *u.PlanCredits > 1e9 || *u.RemainingCredits < 0 || *u.RemainingCredits > 1e9 ||
		float64(int64(*u.PlanCredits)) != *u.PlanCredits || float64(int64(*u.RemainingCredits)) != *u.RemainingCredits {
		return firecrawlAccount{}, firecrawl.ErrResponse
	}
	return firecrawlAccount{int64(*u.PlanCredits), int64(*u.RemainingCredits), start.UTC().Format(firecrawlDate), end.UTC().Format(firecrawlDate), now.UnixMilli()}, nil
}

func (s firecrawlAccountService) refresh(ctx context.Context, v firecrawlAccountSettings) (firecrawlAccount, error) {
	key, err := s.codec.Decrypt(v.key)
	if err != nil {
		return firecrawlAccount{}, errFirecrawlAccountInput
	}
	hash := sha256.Sum256([]byte(key))
	if hex.EncodeToString(hash[:]) != v.hash {
		return firecrawlAccount{}, errFirecrawlAccountInput
	}
	client, err := firecrawl.New(key, s.transport)
	if err != nil {
		return firecrawlAccount{}, err
	}
	usage, err := client.CreditUsage(ctx)
	if err != nil {
		return firecrawlAccount{}, err
	}
	return parseFirecrawlAccount(usage, time.Now())
}

func (s firecrawlAccountService) save(ctx context.Context, v firecrawlAccountSettings, account *firecrawlAccount, switchMode bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return err
	}
	if generation != v.generation {
		return errFirecrawlAccountChanged
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM firecrawl_settings WHERE id=1 FOR UPDATE").Scan(&revision); err != nil {
		return err
	}
	if revision != v.revision {
		return errFirecrawlAccountChanged
	}
	if account != nil {
		raw, err := json.Marshal(account)
		if err != nil {
			return err
		}
		query := "UPDATE firecrawl_settings SET free_usage_json=?, connection_status='connected', last_error='', checked_at=? WHERE id=1"
		if v.mode == "hobby" {
			query = "UPDATE firecrawl_settings SET hobby_usage_json=?, hobby_connection_status='connected', hobby_last_error='', hobby_checked_at=? WHERE id=1"
		}
		if _, err = tx.ExecContext(ctx, query, string(raw), time.UnixMilli(account.Checked).UTC().Format(firecrawlDate)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO firecrawl_usage_periods(key_hash,period_start,period_end,provider_used_high,remaining_credits,plan_credits,checked_at)
 VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE provider_used_high=GREATEST(provider_used_high,VALUES(provider_used_high)),
 remaining_credits=IF(checked_at<=VALUES(checked_at),VALUES(remaining_credits),remaining_credits),
 plan_credits=IF(checked_at<=VALUES(checked_at),VALUES(plan_credits),plan_credits),checked_at=GREATEST(checked_at,VALUES(checked_at))`,
			v.hash, account.Start, account.End, max(0, account.Plan-account.Remaining), account.Remaining, account.Plan, account.Checked); err != nil {
			return err
		}
	}
	if switchMode {
		if _, err = tx.ExecContext(ctx, `UPDATE firecrawl_settings SET mode=?,proxy='basic',enhanced_allowed=0,
 enabled=CASE WHEN ?='free' AND key_encrypted='' THEN 0 ELSE enabled END,revision=revision+1 WHERE id=1`, v.mode, v.mode); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s firecrawlAccountService) budget(ctx context.Context, v firecrawlAccountSettings) (map[string]any, error) {
	limit, rate, browsers := 1000, 10, 2
	if v.mode == "hobby" {
		limit, rate, browsers = 5000, 100, 5
	}
	result := map[string]any{"mode": v.mode, "limits": map[string]int{"credits": limit, "scrapePerMinute": rate, "browsers": browsers, "appConcurrency": 1},
		"status": "unknown", "reason": "Актуальная статистика недоступна. Обновите её.", "checkedAt": nil, "fresh": false, "budget": nil,
		"connectionStatus": v.status, "configured": v.key != "", "subscriptionManagedExternally": true, "autoTopUpAllowed": false, "accountPlanConfirmed": false}
	if v.key == "" {
		result["status"] = "missing_key"
		result["reason"] = "Сохраните ключ выбранного профиля."
		return result, nil
	}
	var account firecrawlAccount
	now := time.Now()
	if json.Unmarshal([]byte(v.usage), &account) != nil || account.Checked <= 0 || v.hash == "" {
		return result, nil
	}
	result["checkedAt"] = account.Checked
	start, a := time.Parse(time.RFC3339Nano, account.Start)
	end, b := time.Parse(time.RFC3339Nano, account.End)
	if a != nil || b != nil || now.Before(start) || !now.Before(end) || account.Checked > now.UnixMilli() || now.UnixMilli()-account.Checked > 60000 {
		return result, nil
	}
	var high, reported, estimated, reserved float64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT provider_used_high FROM firecrawl_usage_periods WHERE key_hash=? AND period_start=? AND period_end=?),?),
 COALESCE(SUM(charged_credits),0),COALESCE(SUM(IF(charged_credits IS NULL,reserved_credits,0)),0),
 COALESCE(SUM(IF(outcome='reserved' AND active_until>?,reserved_credits,0)),0)
 FROM firecrawl_usage_events WHERE period_start=? AND period_end=? AND (profile=? OR key_hash=?)`, v.hash, account.Start, account.End, max(0, account.Plan-account.Remaining),
		now.UnixMilli(), account.Start, account.End, v.mode, v.hash).Scan(&high, &reported, &estimated, &reserved)
	if err != nil {
		return nil, err
	}
	effective := high + reported + estimated
	remaining := max(0, min(float64(limit)-effective, float64(account.Remaining)-reserved))
	var providerEstimate any = max(0, account.Plan-account.Remaining)
	reason := ""
	switch {
	case account.Remaining > account.Plan || (account.Plan != 1000 && account.Plan != 5000):
		providerEstimate = nil
		reason = "API не подтвердил включённый бюджет Firecrawl."
	case v.mode == "hobby" && account.Plan != 5000:
		reason = "API не подтвердил пакет 5000 кредитов для Hobby."
	case v.mode != v.selected:
		reason = "Этот профиль не выбран."
	case !v.enabled:
		reason = "Firecrawl выключен."
	case remaining < 4:
		reason = "Лимит кредитов Firecrawl исчерпан."
	}
	result["fresh"] = true
	result["status"] = "available"
	if reason != "" {
		result["status"] = "blocked"
	}
	result["reason"] = reason
	result["accountPlanConfirmed"] = v.mode == "hobby" && account.Plan == 5000 && account.Remaining <= account.Plan
	result["budget"] = map[string]any{"providerRemaining": account.Remaining, "providerPlanCredits": account.Plan, "providerUsedEstimate": providerEstimate,
		"localReported": reported, "localEstimated": estimated, "reserved": reserved, "effectiveUsed": effective, "remaining": remaining,
		"periodStart": account.Start, "periodEnd": account.End, "minimumReserve": 4}
	return result, nil
}

func firecrawlAccountHandler(manager *auth.Manager, s firecrawlAccountService) http.HandlerFunc {
	return RequireSession(manager, func(w http.ResponseWriter, r *http.Request) {
		user, ok := sessionFromRequest(manager, r)
		if !ok || !user.IsAdmin || (r.Method != http.MethodGet && !writeSameOrigin(r)) {
			jsonReply(w, http.StatusForbidden, map[string]string{"error": "Доступно только администратору."})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		var input struct {
			Mode      string `json:"mode"`
			Revision  *int64 `json:"revision"`
			Confirmed bool   `json:"confirmed"`
		}
		switching := r.URL.Path == "/api/firecrawl/mode"
		if r.Method == http.MethodGet {
			params := r.URL.Query()
			input.Mode = params.Get("mode")
			if len(params) > 0 && !params.Has("mode") || len(params) > 1 || len(params["mode"]) > 1 {
				firecrawlAccountError(w, errFirecrawlAccountInput)
				return
			}
		} else {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || (!switching && (input.Revision != nil || input.Confirmed)) {
				firecrawlAccountError(w, errFirecrawlAccountInput)
				return
			}
		}
		if (input.Mode != "" && input.Mode != "free" && input.Mode != "hobby") ||
			switching && (input.Mode == "" || input.Revision == nil || *input.Revision < 0 || *input.Revision >= 9007199254740991 || input.Mode == "hobby" && !input.Confirmed) {
			firecrawlAccountError(w, errFirecrawlAccountInput)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		settings, err := s.read(ctx, input.Mode)
		if err != nil {
			firecrawlAccountError(w, err)
			return
		}
		if switching && settings.revision != *input.Revision {
			firecrawlAccountError(w, errFirecrawlAccountChanged)
			return
		}
		if r.Method == http.MethodGet {
			view, err := s.budget(ctx, settings)
			if err != nil {
				firecrawlAccountError(w, err)
				return
			}
			jsonReply(w, http.StatusOK, view)
			return
		}
		var account *firecrawlAccount
		if !switching || settings.mode == "hobby" {
			fresh, err := s.refresh(ctx, settings)
			if err != nil {
				firecrawlAccountError(w, err)
				return
			}
			if switching && (fresh.Plan != 5000 || fresh.Remaining > fresh.Plan) {
				firecrawlAccountError(w, errFirecrawlAccountInput)
				return
			}
			account = &fresh
		}
		if err := s.save(ctx, settings, account, switching); err != nil {
			firecrawlAccountError(w, err)
			return
		}
		if switching {
			jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if r.URL.Path == "/api/firecrawl/check" {
			jsonReply(w, http.StatusOK, map[string]any{"ok": true, "remainingCredits": account.Remaining, "account": account})
			return
		}
		raw, _ := json.Marshal(account)
		settings.usage = string(raw)
		settings.status = "connected"
		view, err := s.budget(ctx, settings)
		if err != nil {
			firecrawlAccountError(w, err)
			return
		}
		jsonReply(w, http.StatusOK, view)
	})
}

func firecrawlAccountError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, "Не удалось получить статистику Firecrawl."
	switch {
	case errors.Is(err, errFirecrawlAccountChanged):
		status, message = http.StatusConflict, "Настройки Firecrawl изменились. Обновите страницу и повторите."
	case errors.Is(err, errFirecrawlAccountInput):
		status, message = http.StatusBadRequest, "Проверьте ключ, профиль и подтверждение тарифа Hobby."
	case errors.Is(err, firecrawl.ErrRejected):
		status, message = http.StatusBadGateway, "Firecrawl отклонил запрос статистики. Проверьте API-ключ."
	}
	jsonReply(w, status, map[string]string{"error": message})
}
