package parser

import (
	"encoding/json"
	"os"
	"testing"
)

func checkLeafStageHashes(t *testing.T, raw []byte) {
	t.Helper()
	var frames []struct {
		Provider, Prompt, Title, Markdown string
		RawStored                         json.RawMessage `json:"stored"`
	}
	if json.Unmarshal(raw, &frames) != nil || len(frames) != 4 {
		t.Fatal("source corpus")
	}
	parent := int64(990101)
	allowed := []leafAllowed{{990102, "CI_ROLE → CI_ONE", "CI_ONE", "CI_LEAF_1", &parent}, {990103, "CI_ROLE → CI_TWO", "CI_TWO", "CI_LEAF_2", &parent}}
	tree, e := canonicalHash(allowed)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range frames {
		var stored struct {
			InputHash  string `json:"input_hash"`
			TreeHash   string `json:"tree_hash"`
			PromptHash string `json:"prompt_version"`
			ConfigHash string `json:"config_hash"`
		}
		if json.Unmarshal(f.RawStored, &stored) != nil {
			t.Fatal("source stored")
		}
		cfg := []string{"openai", "gpt-6-luna", "best_free", "medium"}
		mode := 1
		if f.Provider == "jev" {
			cfg = []string{"openrouter", "gpt-6-luna", "jev", "medium"}
			mode = 2
		}
		config, e := canonicalHash(cfg)
		if e != nil {
			t.Fatal(e)
		}
		prompt := stageHash(f.Prompt)
		hash, e := canonicalHash([]any{mode, int64(990001), stageHash(f.Title), int64(1), "CI_SNAPSHOT", stageHash(f.Markdown), tree, [][]any{{int64(990101), stageHash("CI_ROLE_RULES")}}, prompt, config})
		if e != nil {
			t.Fatal(e)
		}
		if hash != stored.InputHash || tree != stored.TreeHash || prompt != stored.PromptHash || config != stored.ConfigHash {
			t.Fatalf("source hashes differ for %s", f.Provider)
		}
	}
}

func TestLeafStageSourceFixture(t *testing.T) {
	raw, e := os.ReadFile("testdata/leaf-role-request-contract.json")
	if e != nil {
		t.Fatal(e)
	}
	checkLeafStageHashes(t, raw)
}
