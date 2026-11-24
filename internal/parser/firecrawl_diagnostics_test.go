package parser

import (
	"errors"
	"strings"
	"testing"
)

func TestFirecrawlFailureDetailsExposeOnlyKnownStage(t *testing.T) {
	cause := errors.New("synthetic upstream private data")
	err := errors.Join(firecrawlFailure("firecrawl_settlement", cause), firecrawlFailure("firecrawl_scrape", cause))
	code, message := firecrawlFailureDetails(err)
	if code != "firecrawl_settlement" || message == "" || strings.Contains(message+err.Error(), cause.Error()) || !errors.Is(err, cause) {
		t.Fatalf("incorrect failure projection: %q %q", code, message)
	}
	if code, _ := firecrawlFailureDetails(cause); code != "" {
		t.Fatal("unclassified error was exposed")
	}
	if !onlyAdmissionCause(firecrawlFailure("firecrawl_reserve", ErrWorkerDrainBeforePaid), ErrWorkerDrainBeforePaid) {
		t.Fatal("wrapping changed graceful drain handling")
	}
}
