package vacancies

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseUpdateRequestAndRevision(t *testing.T) {
	version := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, body     string
		bad            bool
		letter, source bool
	}{
		{"simple edit", `{"revision":"` + version + `","title":"Change"}`, false, false, false},
		{"personal letter", `{"revision":"` + version + `","coverLetterScope":"personal","coverLetterSubject":"Me","coverLetterMd":"My text"}`, false, true, false},
		{"shared letter", `{"revision":"` + version + `","coverLetterScope":"shared","coverLetterSubject":"Team","coverLetterMd":"Team text"}`, false, true, false},
		{"channel detach", `{"revision":"` + version + `","telegramChannelId":null}`, false, false, true},
		{"missing companion", `{"revision":"` + version + `","coverLetterMd":"text"}`, true, false, false},
		{"missing scope", `{"revision":"` + version + `","coverLetterSubject":"Hi","coverLetterMd":"abc"}`, true, false, false},
		{"null letter", `{"revision":"` + version + `","coverLetterScope":"personal","coverLetterSubject":null,"coverLetterMd":"abc"}`, true, false, false},
		{"unrecognized scope", `{"revision":"` + version + `","coverLetterScope":"private","coverLetterSubject":"Hi","coverLetterMd":"abc"}`, true, false, false},
		{"array", `[]`, true, false, false},
		{"not JSON", `{broken`, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseUpdateRequest([]byte(tc.body))
			if tc.bad {
				if !errors.Is(err, ErrInvalidVacancyInput) {
					t.Fatalf("expected validation error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.LetterProvided != tc.letter || got.TelegramProvided != tc.source {
				t.Fatalf("wrong flags: %+v", got)
			}
			if got.TelegramProvided && got.TelegramChannelID != nil {
				t.Fatal("explicit detach did not clear channel")
			}
			if err = ValidateRevision(got.Revision); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, rev := range []string{"", strings.Repeat("x", 64), strings.Repeat("A", 64), strings.Repeat("0", 63), strings.Repeat("0", 65)} {
		if !errors.Is(ValidateRevision(rev), ErrRevisionRequired) {
			t.Errorf("accepted malformed revision: %q", rev)
		}
	}
	if !json.Valid([]byte(`{"revision":"` + version + `"}`)) {
		t.Fatal("test JSON invalid")
	}
}
