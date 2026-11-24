package firecrawl

import "testing"

func TestFreeCreditAllowance(t *testing.T) {
	for _, tc := range []struct {
		mode            string
		plan, remaining int64
		allowed         bool
		used            int64
	}{
		{"free", 1000, 1025, true, 0},
		{"free", 1000, 1026, false, 0},
		{"free", 1000, 999, true, 26},
		{"free", 1025, 1025, true, 0},
		{"hobby", 5000, 5001, false, 0},
		{"hobby", 5000, 4999, true, 1},
		{"hobby", 1000, 1025, false, 0},
	} {
		if SupportsCreditBalance(tc.mode, tc.plan, tc.remaining) != tc.allowed || ProviderCreditsUsed(tc.plan, tc.remaining) != tc.used {
			t.Errorf("unexpected policy for %+v", tc)
		}
	}
}
