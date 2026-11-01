package apifyruns

import (
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"testing"
)

func TestListingOperationKeySurvivesDetailSupport(t *testing.T) {
	v := apifysettings.OperationVersion{CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456"}
	got, err := operationKey(v, "CI operation", apify.HHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "go398:607e218333a572ce63b787ecf901bbc8ec4bcd3143bea87b921dc2c40a2e5839" {
		t.Fatal("existing listing paid identity changed", got)
	}
}
func TestDetailOperationBindsCandidateVersionAndTarget(t *testing.T) {
	v := apifysettings.OperationVersion{CompanyID: 991302, SourceRevision: 1, SourceURL: "https://hh.ru/employer/456", CandidateID: 991305, TitleRevision: 1, VacancyURL: "https://hh.ru/vacancy/123"}
	got, err := operationKey(v, "CI detail", apify.HHOptions{Detail: true})
	if err != nil {
		t.Fatal(err)
	}
	v.TitleRevision++
	next, err := operationKey(v, "CI detail", apify.HHOptions{Detail: true})
	if err != nil || next == got {
		t.Fatal("detail title revision reused paid input identity", err)
	}
	if _, err = operationKey(v, "CI detail", apify.HHOptions{}); err == nil {
		t.Fatal("detail grant used for listing")
	}
}
