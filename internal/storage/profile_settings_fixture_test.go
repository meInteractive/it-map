//go:build mariadb && profilewrites

package storage

import (
	"context"
	"testing"
	"time"

	"itmap/internal/profiles"
)

type profileSettingsFixture struct{ *profileResumeFixture }

var profileSharedTables = []string{"system_prompts", "prompt_model_settings", "vacancy_detail_settings", "vacancy_role_settings"}

func openProfileSettingsFixture(t *testing.T) *profileSettingsFixture {
	t.Helper()
	f := &profileSettingsFixture{profileResumeFixture: openProfileResumeFixture(t)}
	for _, table := range profileSharedTables {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("shared settings fixture requires empty synthetic tables")
		}
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, table := range profileSharedTables {
			key := "id=1"
			if table == "system_prompts" || table == "prompt_model_settings" {
				key = "kind IN ('cover_letter','relevance')"
			}
			if _, err := f.db.ExecContext(cleanup, "DELETE FROM "+table+" WHERE "+key); err != nil {
				t.Error("cannot clean synthetic shared settings")
			}
			var n int
			if err := f.db.QueryRowContext(cleanup, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 0 {
				t.Error("shared settings fixture leaked rows")
			}
		}
	})
	return f
}
func (f *profileSettingsFixture) prompts() profiles.PromptCollection {
	f.t.Helper()
	v, err := f.repo.ReadPrompts(f.ctx)
	if err != nil {
		f.t.Fatalf("shared prompts read failed: %T", err)
	}
	return v
}
func (f *profileSettingsFixture) models() map[string]profiles.Model {
	f.t.Helper()
	v, err := f.repo.ReadModels(f.ctx)
	if err != nil {
		f.t.Fatalf("shared models read failed: %T", err)
	}
	return v
}
func (f *profileSettingsFixture) savePrompt(kind, text string, revision int64) profiles.Prompt {
	f.t.Helper()
	v, err := f.repo.SavePrompt(f.ctx, kind, profiles.TextInput{Text: text, Revision: revision})
	if err != nil {
		f.t.Fatalf("shared prompt save failed: %T", err)
	}
	return v
}
func (f *profileSettingsFixture) saveModel(kind string, input profiles.ModelInput) profiles.Model {
	f.t.Helper()
	v, err := f.repo.SaveModel(f.ctx, kind, input)
	if err != nil {
		f.t.Fatalf("shared model save failed: %T", err)
	}
	return v
}
func freeProfileModel(revision int64) profiles.ModelInput {
	return profiles.ModelInput{Provider: "openrouter", OpenAIModel: "CI_model:1", OpenRouterModel: "best_free", ReasoningEffort: "high", Revision: revision}
}
func (f *profileSettingsFixture) assertEmpty() {
	f.t.Helper()
	for _, table := range profileSharedTables {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			f.t.Fatal("failed settings request initialized persistent rows")
		}
	}
}
