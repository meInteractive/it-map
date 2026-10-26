//go:build mariadb && profilewrites

package storage

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"itmap/internal/httpapi"
	"itmap/internal/profiles"
)

type syntheticProfileCatalog struct {
	calls int
	err   error
	last  profiles.ModelInput
	kind  string
}

func (c *syntheticProfileCatalog) ValidateChoice(_ context.Context, kind string, input profiles.ModelInput) error {
	c.calls++
	c.kind, c.last = kind, input
	return c.err
}

func openProfileSettingsHTTP(f *profileSettingsFixture, writer profiles.Repository, catalog httpapi.ProfileModelValidator) *profileHTTP {
	f.t.Helper()
	return openProfileHTTP(f.profileResumeFixture, writer, func(opts *httpapi.Options) {
		opts.ProfileSettings, opts.ProfileModelCatalog = writer, catalog
	})
}

func modelPayload(input profiles.ModelInput) map[string]any {
	return map[string]any{"provider": input.Provider, "openaiModel": input.OpenAIModel, "openrouterModel": input.OpenRouterModel, "reasoningEffort": input.ReasoningEffort, "revision": input.Revision}
}

func TestMariaDBProfileSettingsHTTPDisposable(t *testing.T) {
	const prompts, models = "/api/profile/prompts", "/api/profile/prompt-models"
	t.Run("admin dispatch defaults and denied requests do not initialize data", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		h := openProfileSettingsHTTP(f, f.repo, nil)
		for _, surface := range []string{prompts, models} {
			h.error(h.call("", "GET", surface, nil, 401))
			for _, user := range []string{"demo", "nastya"} {
				h.error(h.call(user, "GET", surface, nil, 403))
				h.error(h.call(user, "PUT", surface+"/vacancy_role", `{}`, 403))
			}
			h.error(h.call("eugene", "GET", surface+"/vacancy_role", nil, 405))
			h.error(h.call("eugene", "PUT", surface, `{}`, 404))
			h.error(h.call("eugene", "PUT", surface+"/unknown", `{}`, 404))
			h.error(h.call("eugene", "PUT", surface+"/vacancy_role", `{}`, 403, http.Header{"Origin": {"https://cross-site.invalid"}}))
		}
		got := profileJSON[profiles.PromptCollection](t, h.call("eugene", "GET", prompts, nil, 200))
		if !reflect.DeepEqual(got, profiles.DefaultPrompts()) {
			t.Fatal("HTTP prompt defaults differ from legacy")
		}
		gotModels := profileJSON[struct{ Models map[string]profiles.Model }](t, h.call("eugene", "GET", models, nil, 200))
		if !reflect.DeepEqual(gotModels.Models, profiles.DefaultModels()) {
			t.Fatal("HTTP model collection wrapper/defaults differ from legacy")
		}
		f.assertEmpty()
	})
	t.Run("four prompt kinds roundtrip Unicode clear and independent model CAS", func(t *testing.T) {
		for _, kind := range []string{"cover_letter", "relevance", "vacancy_extraction", "vacancy_role"} {
			t.Run(kind, func(t *testing.T) {
				f := openProfileSettingsFixture(t)
				h := openProfileSettingsHTTP(f, f.repo, nil)
				promptPath, modelPath := prompts+"/"+kind, models+"/"+kind
				for _, raw := range []string{`{"markdown":"CI","revision":0,"userName":"Demo"}`, `{"markdown":"CI\u0000","revision":0}`, `{"markdown":"CI","revision":null}`} {
					h.error(h.call("eugene", "PUT", promptPath, raw, 400))
				}
				f.assertEmpty()
				text := strings.Repeat("😀", profiles.PromptLimit(kind))
				payload := map[string]any{"markdown": text, "revision": 0}
				created := profileJSON[profiles.Prompt](t, h.call("eugene", "PUT", promptPath, payload, 200, http.Header{"Content-Type": {"text/plain"}}))
				if created.Kind != kind || created.Markdown != text || created.Revision != 1 || created.MaxLength != profiles.PromptLimit(kind) || created.UpdatedAt == nil {
					t.Fatal("prompt HTTP lost text/revision/kind/limit")
				}
				h.error(h.call("eugene", "PUT", promptPath, payload, 409))
				h.error(h.call("eugene", "PUT", promptPath, map[string]any{"markdown": text + "x", "revision": 1}, 400))
				choice := modelPayload(freeProfileModel(0))
				saved := profileJSON[profiles.Model](t, h.call("eugene", "PUT", modelPath, choice, 200))
				if saved.Revision != 1 || saved.Provider != "openrouter" || saved.OpenAIModel != "CI_model:1" || saved.OpenRouterModel != "best_free" || saved.ReasoningEffort != "high" || saved.UpdatedAt == nil {
					t.Fatal("model HTTP lost independent revision/fields")
				}
				h.error(h.call("eugene", "PUT", modelPath, choice, 409))
				cleared := profileJSON[profiles.Prompt](t, h.call("eugene", "PUT", promptPath, `{"markdown":"","revision":1.0}`, 200))
				if cleared.Revision != 2 || cleared.Markdown != "" {
					t.Fatal("prompt HTTP clear failed")
				}
				got := profileJSON[profiles.PromptCollection](t, h.call("eugene", "GET", prompts, nil, 200))
				gotModels := profileJSON[struct{ Models map[string]profiles.Model }](t, h.call("eugene", "GET", models, nil, 200))
				if got.Prompts[kind].Revision != 2 || got.Prompts[kind].Markdown != "" || gotModels.Models[kind].Revision != 1 || gotModels.Models[kind].OpenAIModel != saved.OpenAIModel {
					t.Fatal("shared HTTP edit changed the other revision/fields")
				}
				h.error(h.call("eugene", "PUT", promptPath, strings.Repeat(" ", int(profiles.PromptBodyLimit(kind))+1), 413, http.Header{"Transfer-Encoding": {"chunked"}}))
				h.error(h.call("eugene", "PUT", modelPath, strings.Repeat(" ", 4097), 413, http.Header{"Transfer-Encoding": {"chunked"}}))
				if !reflect.DeepEqual(got, f.prompts()) || !reflect.DeepEqual(gotModels.Models, f.models()) {
					t.Fatal("oversized settings stream changed rows")
				}
				if !reflect.DeepEqual(f.read("Eugene"), f.original) || f.count(`SELECT COUNT(*) FROM user_profiles WHERE user_name IN ('Demo','Nastya')`) != 0 {
					t.Fatal("shared settings modified personal resumes")
				}
			})
		}
	})
	t.Run("provider choice requires catalog before SQL with synthetic mock only", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		h := openProfileSettingsHTTP(f, f.repo, nil)
		openai, jev := freeProfileModel(0), freeProfileModel(0)
		openai.Provider, openai.OpenAIModel = "openai", "CI_synthetic_model"
		jev.OpenRouterModel = "jev"
		h.error(h.call("eugene", "PUT", models+"/cover_letter", modelPayload(openai), 503))
		h.error(h.call("eugene", "PUT", models+"/relevance", modelPayload(jev), 503))
		f.assertEmpty()
		catalog := &syntheticProfileCatalog{err: &profiles.InputError{Status: 400, Message: "CI synthetic model unavailable"}}
		h = openProfileSettingsHTTP(f, f.repo, catalog)
		h.error(h.call("eugene", "PUT", models+"/cover_letter", modelPayload(openai), 400))
		if catalog.calls != 1 || catalog.last != openai || catalog.kind != "cover_letter" {
			t.Fatal("catalog did not receive exact model choice before SQL")
		}
		f.assertEmpty()
		catalog.err = errors.New("CI_PROVIDER_DETAILS_NOT_PUBLIC")
		data := h.call("eugene", "PUT", models+"/relevance", modelPayload(jev), 503)
		if strings.Contains(string(data), catalog.err.Error()) {
			t.Fatal("provider validation error leaked details")
		}
		f.assertEmpty()
		catalog.err = nil
		for _, kind := range []string{"cover_letter", "vacancy_extraction"} {
			saved := profileJSON[profiles.Model](t, h.call("eugene", "PUT", models+"/"+kind, modelPayload(openai), 200))
			if saved.Provider != "openai" || saved.OpenAIModel != openai.OpenAIModel || saved.Revision != 1 {
				t.Fatal("validated synthetic OpenAI choice not stored")
			}
		}
		for _, kind := range []string{"relevance", "vacancy_role"} {
			saved := profileJSON[profiles.Model](t, h.call("eugene", "PUT", models+"/"+kind, modelPayload(jev), 200))
			if saved.OpenRouterModel != "jev" || saved.Revision != 1 {
				t.Fatal("validated synthetic JEV choice not stored")
			}
		}
		calls := catalog.calls
		h.error(h.call("eugene", "PUT", models+"/cover_letter", modelPayload(jev), 400))
		if catalog.calls != calls {
			t.Fatal("invalid JEV kind reached catalog")
		}
	})
	t.Run("late shared SQL update is generic503 rollback and same revision retry", func(t *testing.T) {
		f := openProfileSettingsFixture(t)
		f.savePrompt("vacancy_role", "CI preserved text", 0)
		f.saveModel("vacancy_role", freeProfileModel(0))
		beforePrompts, beforeModels := f.prompts(), f.models()
		fault, db := openMutationFaultPool(t, "UPDATE vacancy_role_settings SET")
		h := openProfileSettingsHTTP(f, profiles.Repository{DB: db}, nil)
		payload := map[string]any{"markdown": "CI interrupted edit", "revision": 1}
		data := h.call("eugene", "PUT", prompts+"/vacancy_role", payload, 503)
		h.error(data)
		if strings.Contains(string(data), errMutationFault.Error()) {
			t.Fatal("settings API disclosed SQL details")
		}
		fault.mu.Lock()
		fired, commits := fault.fired, fault.commits
		fault.mu.Unlock()
		if !fired || commits != 0 || !reflect.DeepEqual(beforePrompts, f.prompts()) || !reflect.DeepEqual(beforeModels, f.models()) {
			t.Fatal("settings HTTP did not roll back actual SQL before commit")
		}
		saved := profileJSON[profiles.Prompt](t, h.call("eugene", "PUT", prompts+"/vacancy_role", payload, 200))
		if saved.Revision != 2 || saved.Markdown != "CI interrupted edit" || f.models()["vacancy_role"].Revision != 1 {
			t.Fatal("settings HTTP retry failed or changed model revision")
		}
	})
}
