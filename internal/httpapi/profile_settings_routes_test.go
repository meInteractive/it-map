package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/auth"
	"itmap/internal/profiles"
)

type settingsProbe struct {
	reads, prompts, models int
	kind                   string
	text                   profiles.TextInput
	model                  profiles.ModelInput
	err                    error
}

func (p *settingsProbe) ReadPrompts(context.Context) (profiles.PromptCollection, error) {
	p.reads++
	return profiles.DefaultPrompts(), p.err
}
func (p *settingsProbe) ReadModels(context.Context) (map[string]profiles.Model, error) {
	p.reads++
	return profiles.DefaultModels(), p.err
}
func (p *settingsProbe) SavePrompt(_ context.Context, kind string, input profiles.TextInput) (profiles.Prompt, error) {
	p.prompts++
	p.kind = kind
	p.text = input
	return profiles.Prompt{Kind: kind, Markdown: input.Text, Revision: input.Revision + 1, MaxLength: profiles.PromptLimit(kind)}, p.err
}
func (p *settingsProbe) SaveModel(_ context.Context, kind string, input profiles.ModelInput) (profiles.Model, error) {
	p.models++
	p.kind = kind
	p.model = input
	return profiles.Model{Kind: kind, Provider: input.Provider, OpenAIModel: input.OpenAIModel, OpenRouterModel: input.OpenRouterModel, ReasoningEffort: input.ReasoningEffort, Revision: input.Revision + 1}, p.err
}

type modelCatalogProbe struct {
	calls int
	err   error
}

func (p *modelCatalogProbe) ValidateChoice(context.Context, string, profiles.ModelInput) error {
	p.calls++
	return p.err
}
func TestProfileSettingsRouteContract(t *testing.T) {
	manager, err := auth.NewManager(strings.Repeat("s", 64), map[string]string{"eugene": "CI_ADMIN_PASSWORD", "demo": "CI_PASSWORD"})
	if err != nil {
		t.Fatal(err)
	}
	cookies := map[string]*http.Cookie{}
	for _, name := range []string{"eugene", "demo"} {
		password := "CI_PASSWORD"
		if name == "eugene" {
			password = "CI_ADMIN_PASSWORD"
		}
		user, _ := manager.Authenticate(name, password)
		token, err := manager.Issue(user, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		cookies[name] = &http.Cookie{Name: auth.CookieName, Value: token}
	}
	store := &settingsProbe{}
	catalog := &modelCatalogProbe{}
	handler := NewHandlerWithOptions(nil, Options{Auth: manager, ProfileSettings: store, ProfileModelCatalog: catalog})
	call := func(h http.Handler, user, method, path, body string, headers http.Header) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://itmap.test"+path, strings.NewReader(body))
		if user != "" {
			r.AddCookie(cookies[user])
		}
		for key, values := range headers {
			r.Header[key] = values
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, surface := range []string{"prompts", "prompt-models"} {
		path := "/api/profile/" + surface
		if w := call(handler, "", "GET", path, "", nil); w.Code != 401 {
			t.Fatal("anonymous shared settings read allowed")
		}
		for _, suffix := range []string{"", "/relevance", "/CI_unknown"} {
			if w := call(handler, "demo", "PUT", path+suffix, `{}`, nil); w.Code != 403 {
				t.Fatal("member shared settings access allowed")
			}
		}
		if w := call(handler, "eugene", "GET", path, "", nil); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("admin settings read failed")
		}
		if w := call(handler, "eugene", "PUT", path+"/CI_unknown", `{}`, nil); w.Code != 404 {
			t.Fatal("unknown settings kind accepted")
		}
		for _, suffix := range []string{"/", "/%72elevance", "/relevance/"} {
			if w := call(handler, "eugene", "GET", path+suffix, "", nil); w.Code != 404 {
				t.Fatal("trailing slash/encoded kind was accepted as collection/item")
			}
		}
		if w := call(handler, "eugene", "GET", path+"/relevance", "", nil); w.Code != 405 {
			t.Fatal("settings item method contract changed")
		}
	}
	text := `{"markdown":"  CI prompt 😀 <&>  ","revision":0}`
	if w := call(handler, "eugene", "PUT", "/api/profile/prompts/cover_letter", text, http.Header{"Origin": {"https://cross-site.invalid"}}); w.Code != 403 || store.prompts != 0 {
		t.Fatal("cross-origin prompt write allowed")
	}
	if w := call(handler, "eugene", "PUT", "/api/profile/prompts/cover_letter", text, http.Header{"Content-Type": {"text/plain"}}); w.Code != 200 || store.text.Text != "  CI prompt 😀 <&>  " || store.kind != "cover_letter" {
		t.Fatal("shared prompt text/limit/source MIME contract changed")
	}
	var row map[string]any
	w := call(handler, "eugene", "PUT", "/api/profile/prompts/vacancy_role", `{"markdown":"","revision":0}`, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &row) != nil || row["maxLength"] != float64(5000) {
		t.Fatal("prompt write reply maxLength changed")
	}
	writes := store.prompts
	for _, body := range []string{`{"markdown":"CI","revision":0,"userName":"Demo"}`, `{"markdown":"CI\u0000","revision":0}`, `{"markdown":"CI","revision":null}`} {
		if w := call(handler, "eugene", "PUT", "/api/profile/prompts/relevance", body, nil); w.Code != 400 || store.prompts != writes {
			t.Fatal("invalid shared prompt reached storage")
		}
	}
	free := `{"provider":"openrouter","openaiModel":"","reasoningEffort":"medium","revision":0}`
	if w := call(handler, "eugene", "PUT", "/api/profile/prompt-models/relevance", free, nil); w.Code != 200 || store.model.OpenRouterModel != "best_free" || catalog.calls != 0 {
		t.Fatal("free source model selection unexpectedly required external work")
	}
	live := `{"provider":"openai","openaiModel":"CI_model","reasoningEffort":"auto","revision":0}`
	unavailable := NewHandlerWithOptions(nil, Options{Auth: manager, ProfileSettings: store})
	models := store.models
	if w := call(unavailable, "eugene", "PUT", "/api/profile/prompt-models/relevance", live, nil); w.Code != 503 || store.models != models {
		t.Fatal("model availability gate bypassed when not configured")
	}
	if w := call(handler, "eugene", "PUT", "/api/profile/prompt-models/relevance", live, nil); w.Code != 200 || catalog.calls != 1 || store.models != models+1 {
		t.Fatal("configured model validator not called")
	}
	catalog.err = &profiles.InputError{Status: 400, Message: "CI unavailable model"}
	models = store.models
	if w := call(handler, "eugene", "PUT", "/api/profile/prompt-models/relevance", live, nil); w.Code != 400 || store.models != models {
		t.Fatal("provider validation failure still saved a model")
	}
	jev := `{"provider":"openrouter","openaiModel":"","openrouterModel":"jev","reasoningEffort":"medium","revision":0}`
	if w := call(unavailable, "eugene", "PUT", "/api/profile/prompt-models/cover_letter", jev, nil); w.Code != 400 {
		t.Fatal("JEV kind restriction lost")
	}
	if w := call(unavailable, "eugene", "PUT", "/api/profile/prompt-models/vacancy_role", jev, nil); w.Code != 503 || store.models != models {
		t.Fatal("JEV catalog validation was bypassed")
	}
	store.err = profiles.ErrConflict
	if w := call(handler, "eugene", "PUT", "/api/profile/prompts/relevance", text, nil); w.Code != 409 {
		t.Fatal("prompt CAS conflict not 409")
	}
	store.err = errors.New("CI_PRIVATE_SQL_DETAILS")
	if w := call(handler, "eugene", "GET", "/api/profile/prompts", "", nil); w.Code != 503 || strings.Contains(w.Body.String(), "CI_PRIVATE_SQL_DETAILS") {
		t.Fatal("shared settings read exposed SQL details")
	}
	without := NewHandlerWithOptions(nil, Options{Auth: manager})
	if w := call(without, "demo", "GET", "/api/profile/prompts", "", nil); w.Code != 403 {
		t.Fatal("storage availability disclosed to member before role guard")
	}
}
