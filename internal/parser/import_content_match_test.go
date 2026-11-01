package parser

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestImportContentSourceParity(t *testing.T) {
	raw, err := exec.Command("node", "../../scripts/check-go-import-match.mjs").Output()
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Title, Body, OtherTitle, OtherBody, URL string
		Expected                                bool
	}
	if err = json.Unmarshal(raw, &cases); err != nil || len(cases) < 6 {
		t.Fatal("source corpus", err)
	}
	for i, x := range cases {
		got := importSameContent(x.URL, x.OtherTitle, x.OtherBody, "https://hh.ru/vacancy/10", x.Title, x.Body)
		if got != x.Expected {
			t.Errorf("case %d: got %v want %v", i, got, x.Expected)
		}
	}
}
