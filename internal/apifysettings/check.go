package apifysettings

import (
	"context"
	"encoding/json"

	"itmap/internal/integrations/apify"
)

// ParseCheck принимает только имя профиля, не принимая токен от браузера.
func ParseCheck(body []byte) (string, error) {
	f, err := inputObject(body, "profile")
	if err != nil || len(f) != 1 {
		return "", ErrInput
	}
	var profile string
	if json.Unmarshal(f["profile"], &profile) != nil || (profile != "free" && profile != "starter") {
		return "", ErrInput
	}
	return profile, nil
}

type CheckSettings interface {
	Generation(context.Context) (string, error)
	Read(context.Context) (PublicView, error)
	Resolve(context.Context, string) (string, error)
}
type AccountReader interface {
	Account(context.Context) (apify.BillingAccount, error)
}

// CheckService читает только аккаунт и лимиты; Actor никогда не запускается.
type CheckService struct {
	Settings  CheckSettings
	NewClient func(string) (AccountReader, error)
}

func (s CheckService) Check(ctx context.Context, profile string) (apify.BillingAccount, error) {
	if s.Settings == nil || (profile != "free" && profile != "starter") {
		return apify.BillingAccount{}, ErrInput
	}
	generation, err := s.Settings.Generation(ctx)
	if err != nil {
		return apify.BillingAccount{}, err
	}
	view, err := s.Settings.Read(ctx)
	if err != nil {
		return apify.BillingAccount{}, err
	}
	if !view.Profiles[profile].Configured {
		return apify.BillingAccount{}, ErrMissing
	}
	token, err := s.Settings.Resolve(ctx, profile)
	if err != nil {
		return apify.BillingAccount{}, err
	}
	validate := func() error {
		current, err := s.Settings.Generation(ctx)
		if err != nil {
			return err
		}
		if current != generation {
			return ErrGeneration
		}
		state, err := s.Settings.Read(ctx)
		if err != nil {
			return err
		}
		if state.Revision != view.Revision || !state.Profiles[profile].Configured {
			return ErrChanged
		}
		return nil
	}
	if err := validate(); err != nil {
		return apify.BillingAccount{}, err
	}
	factory := s.NewClient
	if factory == nil {
		factory = func(token string) (AccountReader, error) { return apify.New(token) }
	}
	client, err := factory(token)
	if err != nil {
		return apify.BillingAccount{}, err
	}
	if client == nil {
		return apify.BillingAccount{}, ErrStorage
	}
	account, err := client.Account(ctx)
	if err != nil {
		return apify.BillingAccount{}, err
	}
	if err := validate(); err != nil {
		return apify.BillingAccount{}, err
	}
	return account, nil
}
