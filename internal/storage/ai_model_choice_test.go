//go:build mariadb && profilewrites

package storage

import (
	"context"
	"itmap/internal/profiles"
	"net/http/httptest"
	"strings"
	"testing"
)

type generationModelCatalog struct {
	f     *profileSettingsFixture
	calls int
}

func (c *generationModelCatalog) ValidateChoice(context.Context, string, profiles.ModelInput) error {
	c.calls++
	c.f.exec("UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000001' WHERE id=1")
	return nil
}
func TestMariaDBAIModelChoiceGenerationDisposable(t *testing.T) {
	f := openProfileSettingsFixture(t)
	g, e := f.repo.Generation(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	catalog := &generationModelCatalog{f: f}
	h := openProfileSettingsHTTP(f, f.repo, catalog)
	input := profiles.ModelInput{Provider: "openai", OpenAIModel: "gpt-6-luna", OpenRouterModel: "best_free", ReasoningEffort: "medium"}
	h.call("eugene", "PUT", "/api/profile/prompt-models/cover_letter", modelPayload(input), 409)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", g)
	f.assertEmpty()
	if catalog.calls != 1 {
		t.Fatal("provider-validation race was not exercised")
	}
	// Restore может произойти и во время медленного чтения body до network.
	h = openProfileSettingsHTTP(f, f.repo, nil)
	r := httptest.NewRequest("PUT", "https://itmap.test/api/profile/prompt-models/cover_letter", nil)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://itmap.test")
	r.AddCookie(h.cookies["eugene"])
	payload := `{"provider":"openrouter","openaiModel":"","openrouterModel":"best_free","reasoningEffort":"auto","revision":0}`
	r.Body = &aiRestoreBody{r: strings.NewReader(payload), before: func() {
		f.exec("UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000001' WHERE id=1")
	}}
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", g)
	if w.Code != 409 {
		t.Fatal("generation not captured before body")
	}
	f.assertEmpty()
	saved, e := f.repo.SaveModelGeneration(f.ctx, g, "cover_letter", input)
	if e != nil || saved.Revision != 1 || saved.OpenAIModel != "gpt-6-luna" {
		t.Fatal("guarded save failed", e)
	}
}
