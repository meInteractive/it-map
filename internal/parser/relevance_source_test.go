//go:build legacyinterop

package parser

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestRelevanceSourceParity(t *testing.T) {
	raw, err := exec.Command("node", "../../scripts/check-relevance-source.mjs").Output()
	if err != nil {
		t.Fatal("legacy relevance oracle", err)
	}
	var source struct {
		Snapshot []any  `json:"snapshot"`
		Hash     string `json:"hash"`
		Answers  []struct {
			Value  string `json:"value"`
			Answer *int   `json:"answer"`
		} `json:"answers"`
	}
	if err = json.Unmarshal(raw, &source); err != nil || len(source.Snapshot) != 18 || len(source.Answers) < 10 {
		t.Fatal("legacy relevance fixture", err)
	}
	hash, err := canonicalHash(source.Snapshot)
	if err != nil || hash != source.Hash {
		t.Fatal("legacy snapshot hash differs", err)
	}
	for _, test := range source.Answers {
		got, ok := parseRelevanceAnswer(test.Value)
		if ok != (test.Answer != nil) || ok && got != *test.Answer {
			t.Errorf("answer %q differs", test.Value)
		}
	}
}
