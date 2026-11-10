package parser

import (
	"encoding/json"
	"testing"

	"itmap/internal/apifysettings"
)

func TestHHVerifiedQueueLineage(t *testing.T) {
	const generation = "00000000-0000-4000-8000-000000000001"
	const source = "https://hh.ru/employer/456"
	from, to := "2026-11-01T00:00:00+03:00", "2026-11-03T23:59:59+03:00"
	base := hhListingCursor{Version: 1, LineageVersion: 1, MoscowFilter: true, DateFilterApplied: true, VacancyAgeDays: 3, DateFrom: &from, DateTo: &to, ProfessionalRoles: []string{"36", "73"}, CandidateIDs: []int64{12, 13}, Operation: apifysettings.OperationVersion{Generation: generation, CompanyID: 7, SourceURL: source, SourceRevision: 2}}
	for _, tc := range []struct {
		name  string
		alter func(*hhListingCursor)
		valid bool
	}{
		{"подтверждённый список", func(*hhListingCursor) {}, true},
		{"без города", func(c *hhListingCursor) { c.MoscowFilter = false }, false},
		{"без возраста", func(c *hhListingCursor) { c.DateFilterApplied = false }, false},
		{"старый поисковый запрос", func(c *hhListingCursor) { s := "NAME:(manager)"; c.TitleQuery = &s; c.ProfessionalRoles = nil }, false},
		{"без ролей", func(c *hhListingCursor) { c.ProfessionalRoles = nil }, false},
		{"повтор кандидата", func(c *hhListingCursor) { c.CandidateIDs = []int64{12, 12} }, false},
		{"чужая компания", func(c *hhListingCursor) { c.Operation.CompanyID = 8 }, false},
		{"чужое поколение", func(c *hhListingCursor) { c.Operation.Generation = "00000000-0000-4000-8000-000000000002" }, false},
		{"изменилась ревизия", func(c *hhListingCursor) { c.Operation.SourceRevision = 3 }, false},
		{"неверная дата", func(c *hhListingCursor) { c.DateTo = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.alter(&c)
			raw, _ := json.Marshal(c)
			got := hhVerifiedLineage(raw, generation, 7, source, 2)
			if (len(got) == 2) != tc.valid {
				t.Fatalf("lineage=%v want valid=%v", got, tc.valid)
			}
		})
	}
}

func TestHHVerifiedQueueEligibility(t *testing.T) {
	hh := "hh"
	company, candidate := int64(7), int64(12)
	job := Job{Status: "pending", Type: "classify_title", SourceID: &hh, CompanyID: &company, CandidateID: &candidate}
	options := ClaimOptions{VerifiedHHOnly: true}
	types := map[string]bool{"classify_title": true}
	view := claimView{verifiedHH: map[hhCandidateKey]bool{{7, 12}: true}}
	if ok, _, e := claimEligible(job, 1, DefaultSettings(), options, types, view); e != nil || !ok {
		t.Fatalf("verified candidate rejected: %v %v", ok, e)
	}
	company = 8
	if ok, _, e := claimEligible(job, 1, DefaultSettings(), options, types, view); e != nil || ok {
		t.Fatalf("another company admitted: %v %v", ok, e)
	}
	options.VerifiedHHOnly = false
	if ok, _, e := claimEligible(job, 1, DefaultSettings(), options, types, view); e != nil || !ok {
		t.Fatalf("explicit scoped path changed: %v %v", ok, e)
	}
}

func TestHHOldScanDoesNotRunInGlobalCycle(t *testing.T) {
	hh := "hh"
	job := Job{ID: "old", Status: "pending", Type: "scan_source", SourceID: &hh, Payload: json.RawMessage(`{}`)}
	view := claimView{blockedHHScans: map[string]bool{"old": true}}
	types := map[string]bool{"scan_source": true}
	if ok, _, err := claimEligible(job, 1, DefaultSettings(), ClaimOptions{VerifiedHHOnly: true}, types, view); err != nil || ok {
		t.Fatalf("old scan admitted: %v %v", ok, err)
	}
	if ok, _, err := claimEligible(job, 1, DefaultSettings(), ClaimOptions{}, types, view); err != nil || !ok {
		t.Fatalf("explicit resume changed: %v %v", ok, err)
	}
}

func TestHHBlockedLineageReasonReadOnlyClassification(t *testing.T) {
	const generation = "00000000-0000-4000-8000-000000000001"
	const source = "https://hh.ru/employer/456"
	from, to := "2026-11-01T00:00:00+03:00", "2026-11-03T23:59:59+03:00"
	base := hhListingCursor{Version: 1, LineageVersion: 1, MoscowFilter: true, DateFilterApplied: true,
		VacancyAgeDays: 3, DateFrom: &from, DateTo: &to, ProfessionalRoles: []string{"36"},
		Operation: apifysettings.OperationVersion{Generation: generation, CompanyID: 7, SourceURL: source, SourceRevision: 2}}
	for _, tc := range []struct {
		name string
		current bool
		alter func(*hhListingCursor)
		raw []byte
		want string
	}{
		{name: "источник изменился", current: false, want: hhBlockedSourceChanged},
		{name: "старые фильтры ролей", current: true, alter: func(c *hhListingCursor) {c.ProfessionalRoles = nil}, want: hhBlockedLegacyFilters},
		{name: "старый формат lineage", current: true, alter: func(c *hhListingCursor) {c.LineageVersion = 0}, want: hhBlockedLegacyFilters},
		{name: "повреждённый JSON", current: true, raw: []byte("{"), want: hhBlockedMalformedCursor},
		{name: "другая версия источника в операции", current: true, alter: func(c *hhListingCursor) {c.Operation.SourceRevision = 3}, want: hhBlockedOtherProof},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			if tc.alter != nil {
				tc.alter(&c)
			}
			raw := tc.raw
			if raw == nil {
				raw, _ = json.Marshal(c)
			}
			got := hhBlockedLineageReason(raw, tc.current)
			if got != tc.want {
				t.Fatalf("причина блокировки %q, ожидалось %q", got, tc.want)
			}
			if tc.current && tc.want != hhBlockedMalformedCursor {
				if _, verified := hhCursorProof(raw, generation, 7, source, 2); verified {
					t.Fatal("классифицированный заблокированный cursor не должен проходить proof")
				}
			}
		})
	}
}
