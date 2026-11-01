package apify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAccountBillingProjectionIsReadOnly(t *testing.T) {
	start, end := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	c, calls := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("billing performed mutation")
		}
		switch r.URL.Path {
		case "/v2/users/me":
			io.WriteString(w, `{"data":{"id":"CIAccount","token":"PRIVATE_SYNTHETIC","email":"PRIVATE_SYNTHETIC","plan":{"id":"FREE","monthlyBasePriceUsd":0,"monthlyUsageCreditsUsd":5,"isEnabled":true}}}`)
		case "/v2/users/me/limits":
			fmt.Fprintf(w, `{"data":{"monthlyUsageCycle":{"startAt":%q,"endAt":%q},"current":{"monthlyUsageUsd":1.5},"limits":{"maxMonthlyUsageUsd":5}}}`, start, end)
		default:
			t.Error("unexpected billing path")
		}
	})
	a, e := c.Account(context.Background())
	if e != nil || a.AccountID != "CIAccount" || a.ProviderUsedUSD != 1.5 || calls.Load() != 2 {
		t.Fatal("billing projection", e)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "PRIVATE_SYNTHETIC") {
		t.Fatal("private account fields exposed")
	}
}

func TestAccountRejectsMissingBilling(t *testing.T) {
	c, _ := mockClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"id":"CIAccount","plan":{}}}`)
	})
	if _, e := c.Account(context.Background()); e == nil {
		t.Fatal("missing billing accepted")
	}
}
