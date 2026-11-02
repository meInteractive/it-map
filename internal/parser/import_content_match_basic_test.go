package parser

import (
	"strings"
	"testing"
)

func TestImportCrossSourceExactContent(t *testing.T) {
	body := strings.Repeat("CI описание обязанностей и требований. ", 12)
	if !importSameContent("https://career.example.org/job/1", "CI title", body, "https://hh.ru/vacancy/123", "CI title", body) {
		t.Fatal("exact cross-source content missed")
	}
	if importSameContent("https://career.example.org/job/1", "CI title", body, "https://hh.ru/vacancy/123", "other title", body) {
		t.Fatal("different title matched")
	}
	if importSameContent("https://career.example.org/job/1", "CI title", "short", "https://hh.ru/vacancy/123", "CI title", "short") {
		t.Fatal("short content matched")
	}
}
