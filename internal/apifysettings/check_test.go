package apifysettings

import (
	"context"
	"errors"
	"testing"

	"itmap/internal/integrations/apify"
)

type checkSettingsFake struct {
	generation string
	revision   int64
	token      string
}

func (f *checkSettingsFake) Generation(context.Context) (string, error) { return f.generation, nil }
func (f *checkSettingsFake) Read(context.Context) (PublicView, error) {
	return PublicView{Revision: f.revision, Profiles: map[string]PublicProfile{"free": {Configured: f.token != ""}}}, nil
}
func (f *checkSettingsFake) Resolve(context.Context, string) (string, error) { return f.token, nil }

type accountFake func(context.Context) (apify.BillingAccount, error)

func (f accountFake) Account(ctx context.Context) (apify.BillingAccount, error) { return f(ctx) }

func TestCheckProfileAndStaleResults(t *testing.T) {
	for _, body := range []string{`{}`, `{"profile":"other"}`, `{"profile":"free","token":"x"}`, `null`} {
		if _, err := ParseCheck([]byte(body)); !errors.Is(err, ErrInput) {
			t.Fatalf("accepted %s", body)
		}
	}
	if p, err := ParseCheck([]byte(`{"profile":"free"}`)); err != nil || p != "free" {
		t.Fatal("valid profile rejected")
	}
	for _, mode := range []string{"success", "revision", "generation"} {
		t.Run(mode, func(t *testing.T) {
			store := &checkSettingsFake{generation: "CI_GENERATION", revision: 3, token: "synthetic"}
			calls := 0
			svc := CheckService{Settings: store, NewClient: func(token string) (AccountReader, error) {
				if token != "synthetic" {
					t.Fatal("incorrect key")
				}
				return accountFake(func(context.Context) (apify.BillingAccount, error) {
					calls++
					if mode == "revision" {
						store.revision++
					}
					if mode == "generation" {
						store.generation = "OTHER_GENERATION"
					}
					return apify.BillingAccount{AccountID: "CIAccount"}, nil
				}), nil
			}}
			a, err := svc.Check(context.Background(), "free")
			if calls != 1 {
				t.Fatal("account read count", calls)
			}
			switch mode {
			case "success":
				if err != nil || a.AccountID != "CIAccount" {
					t.Fatal("check failed", err)
				}
			case "revision":
				if !errors.Is(err, ErrChanged) || a.AccountID != "" {
					t.Fatal("stale revision accepted")
				}
			case "generation":
				if !errors.Is(err, ErrGeneration) || a.AccountID != "" {
					t.Fatal("stale generation accepted")
				}
			}
		})
	}
}
