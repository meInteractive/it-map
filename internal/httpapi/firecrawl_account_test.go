package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"itmap/internal/legacycrypto"
)

type firecrawlAccountTransport func(*http.Request) (*http.Response, error)

func (f firecrawlAccountTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestFirecrawlAccountRefreshOnlyReadsCredits(t *testing.T) {
	codec, err := legacycrypto.New("synthetic-secret", legacycrypto.Firecrawl)
	if err != nil {
		t.Fatal(err)
	}
	const key = "fc-synthetic-account-key"
	ciphertext, err := codec.Encrypt(key)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(key))
	calls := 0
	service := firecrawlAccountService{codec: codec, transport: firecrawlAccountTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.String() != "https://api.firecrawl.dev/v2/team/credit-usage" ||
			r.Header.Get("Authorization") != "Bearer "+key {
			t.Fatal("unexpected Firecrawl request")
		}
		now := time.Now().UTC()
		body := fmt.Sprintf(`{"success":true,"data":{"planCredits":5000,"remainingCredits":1200,"billingPeriodStart":%q,"billingPeriodEnd":%q}}`,
			now.Add(-24*time.Hour).Format(time.RFC3339), now.Add(24*time.Hour).Format(time.RFC3339))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	account, err := service.refresh(context.Background(), firecrawlAccountSettings{key: ciphertext, hash: hex.EncodeToString(hash[:])})
	if err != nil || calls != 1 || account.Plan != 5000 || account.Remaining != 1200 {
		t.Fatalf("account=%+v calls=%d err=%v", account, calls, err)
	}
	if _, err := service.refresh(context.Background(), firecrawlAccountSettings{key: ciphertext, hash: "wrong"}); err == nil || calls != 1 {
		t.Fatalf("stale hash reached provider: calls=%d err=%v", calls, err)
	}
}
