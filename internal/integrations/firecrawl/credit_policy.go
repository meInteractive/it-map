package firecrawl

const FreePlanCredits = 1000
const FreeCreditLimit = 1025
const HobbyCreditLimit = 5000

// SupportsCreditBalance accepts the bounded Free allowance while preserving
// the provider plan and available balance as separate quantities.
func SupportsCreditBalance(mode string, plan, remaining int64) bool {
	if remaining < 0 {
		return false
	}
	switch mode {
	case "free":
		switch plan {
		case FreePlanCredits, FreeCreditLimit:
			return remaining <= FreeCreditLimit
		case HobbyCreditLimit:
			return remaining <= HobbyCreditLimit
		}
	case "hobby":
		return plan == HobbyCreditLimit && remaining <= HobbyCreditLimit
	}
	return false
}

// ProviderCreditsUsed keeps the same baseline for all snapshots of a Free
// billing period, including after the balance falls below the nominal plan.
func ProviderCreditsUsed(plan, remaining int64) int64 {
	if plan == FreePlanCredits {
		plan = FreeCreditLimit
	}
	return max(0, plan-remaining)
}
