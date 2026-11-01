package apifyruns

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

// Provider не позволяет вызывающему коду подменять Actor, лимит или адрес API.
type Provider interface {
	Account(context.Context) (apify.BillingAccount, error)
	Start(context.Context, string, apify.HHOptions) (apify.ActorRun, error)
	Run(context.Context, string) (apify.ActorRun, error)
}

type Service struct {
	Repository  Repository
	NewClient   func(string) (Provider, error)
	CheckActive func(context.Context) error
}

// WithCheck возвращает отдельную копию для текущей стадии. Callback выполняется
// только вне денежных транзакций и не меняет identity оплаченной операции.
func (s Service) WithCheck(check func(context.Context) error) Service {
	previous := s.CheckActive
	s.CheckActive = func(ctx context.Context) error {
		if previous != nil {
			if err := previous(ctx); err != nil {
				return err
			}
		}
		if check != nil {
			return check(ctx)
		}
		return nil
	}
	return s
}
func (s Service) checkActive(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.CheckActive != nil {
		return s.CheckActive(ctx)
	}
	return nil
}

func (s Service) client(token string) (Provider, error) {
	if s.NewClient != nil {
		return s.NewClient(token)
	}
	return apify.New(token)
}

// Ключ относится к серверному заданию, текущему источнику и точному input.
// Профиль и revision настроек не создают второй денежный журнал для того же input.
func operationKey(v apifysettings.OperationVersion, key string, options apify.HHOptions) (string, error) {
	if key == "" || len(key) > 1024 {
		return "", apifysettings.ErrInput
	}
	if v.CandidateID != 0 && !options.Detail {
		return "", apifysettings.ErrInput
	}
	input, e := apify.HHInput(operationTarget(v), options)
	if e != nil {
		return "", e
	}
	type detailIdentity struct {
		CandidateID, TitleRevision int64
		VacancyURL                 string
	}
	var detail *detailIdentity
	if v.CandidateID != 0 {
		detail = &detailIdentity{v.CandidateID, v.TitleRevision, v.VacancyURL}
	}
	raw, e := json.Marshal(struct {
		CompanyID      int64
		SourceRevision int64
		SourceURL      string
		Key            string
		Input          apify.StartInput
		Detail         *detailIdentity `json:",omitempty"`
	}{v.CompanyID, v.SourceRevision, v.SourceURL, key, input, detail})
	if e != nil {
		return "", apifysettings.ErrInput
	}
	hash := sha256.Sum256(raw)
	return "go398:" + hex.EncodeToString(hash[:]), nil
}

// Execute выполняет один старт либо продолжает известную операцию. Восстановленный
// starting/uncertain без run ID никогда не получает повторный POST.
func (s Service) Execute(ctx context.Context, v apifysettings.OperationVersion, key string, options apify.HHOptions) (Record, error) {
	if err := s.checkActive(ctx); err != nil {
		return Record{}, err
	}
	scoped, e := operationKey(v, key, options)
	if e != nil {
		return Record{}, e
	}
	row, e := s.Repository.readOperation(ctx, v, scoped)
	if e == nil {
		if row.RunID == "" {
			return row, ErrUncertain
		}
		if (row.State == "succeeded" || row.State == "failed") && row.ChargedUSD != nil {
			return row, nil
		}
		return s.reconcile(ctx, v, scoped)
	}
	if !errors.Is(e, errNoOperation) {
		return Record{}, e
	}
	token, e := s.Repository.Settings.ResolveOperation(ctx, v)
	if e != nil {
		return Record{}, e
	}
	client, e := s.client(token)
	if e != nil {
		return Record{}, e
	}
	account, e := client.Account(ctx)
	if e != nil {
		return Record{}, e
	}
	if err := s.checkActive(ctx); err != nil {
		return Record{}, err
	}
	if _, e = s.Repository.StoreAccount(ctx, v, account); e != nil {
		return Record{}, e
	}
	if err := s.checkActive(ctx); err != nil {
		return Record{}, err
	}
	reserved, e := s.Repository.Reserve(ctx, v, scoped)
	if e != nil {
		return Record{}, e
	}
	if reserved.Existing {
		row, e = s.Repository.readOperation(ctx, v, scoped)
		if e != nil {
			return Record{}, e
		}
		if row.RunID == "" {
			return row, ErrUncertain
		}
		return s.reconcile(ctx, v, scoped)
	}
	if err := s.checkActive(ctx); err != nil {
		return Record{}, err
	}
	row, token, e = s.Repository.claimStart(ctx, v, scoped)
	if e != nil {
		return row, e
	}
	// Последняя проверка версий перед сетью; запись результата также fenced.
	if _, e = s.Repository.Settings.ResolveOperation(ctx, v); e != nil {
		return row, e
	}
	client, e = s.client(token)
	if e != nil {
		return row, e
	}
	if err := s.checkActive(ctx); err != nil {
		return row, err
	}
	run, e := client.Start(ctx, operationTarget(v), options)
	if e != nil {
		return row, ErrUncertain
	}
	return s.Repository.record(ctx, v, scoped, run)
}

// Reconcile делает только GET известного run. Без подтверждённой суммы резерв
// остаётся неизвестным; GET-ошибка не меняет состояние или стоимость в журнале.
func (s Service) Reconcile(ctx context.Context, v apifysettings.OperationVersion, key string, options apify.HHOptions) (Record, error) {
	scoped, e := operationKey(v, key, options)
	if e != nil {
		return Record{}, e
	}
	return s.reconcile(ctx, v, scoped)
}
func (s Service) reconcile(ctx context.Context, v apifysettings.OperationVersion, key string) (Record, error) {
	if err := s.checkActive(ctx); err != nil {
		return Record{}, err
	}
	row, e := s.Repository.readOperation(ctx, v, key)
	if e != nil {
		return Record{}, e
	}
	if row.RunID == "" {
		return row, ErrUncertain
	}
	token, e := s.Repository.Settings.ResolveOperation(ctx, v)
	if e != nil {
		return row, e
	}
	client, e := s.client(token)
	if e != nil {
		return row, e
	}
	if err := s.checkActive(ctx); err != nil {
		return row, err
	}
	run, e := client.Run(ctx, row.RunID)
	if e != nil {
		return row, e
	}
	return s.Repository.record(ctx, v, key, run)
}

func operationTarget(v apifysettings.OperationVersion) string {
	if v.CandidateID != 0 {
		return v.VacancyURL
	}
	return v.SourceURL
}
