package aiproviders

import (
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/integrations/aihttp"
	"strings"
	"testing"
)

func TestProviderKeyInputs(t *testing.T) {
	for _, p := range []aihttp.Provider{aihttp.OpenAI, aihttp.OpenRouter} {
		prefix := "sk-"
		if p == aihttp.OpenRouter {
			prefix += "or-"
		}
		key := prefix + strings.Repeat("x", 20)
		raw, _ := json.Marshal(map[string]any{"apiKey": "\ufeff " + key + "\n"})
		got, e := ParseKey(p, raw)
		if e != nil || got != key {
			t.Fatal("valid input rejected")
		}
		for _, b := range []string{`[]`, `null`, `{`, `{}`, `{"apiKey":null}`, `{"apiKey":false}`, `{"apiKey":"wrong"}`, strings.Repeat(" ", 2049)} {
			_, e = ParseKey(p, []byte(b))
			var ie *InputError
			if !errors.As(e, &ie) {
				t.Fatal("invalid input accepted")
			}
			if len(b) > 2048 && ie.Status != 413 {
				t.Fatal("body limit lost")
			}
		}
		raw, _ = json.Marshal(map[string]any{"apiKey": key, "unknown": true})
		_, e = ParseKey(p, raw)
		if (p == aihttp.OpenAI) != (e != nil) {
			t.Fatal("source extra fields policy differs")
		}
	}
	if e := ValidateKey(aihttp.OpenAI, "sk-"+"or-"+strings.Repeat("x", 20)); e == nil {
		t.Fatal("OpenRouter key accepted as OpenAI")
	}
	r := NewRepository(nil, "CI_PRIVATE_SECRET", "CI_PRIVATE_KEY")
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "CI_PRIVATE") || strings.Contains(fmt.Sprintf("%v %#v", r, r), "CI_PRIVATE") {
		t.Fatal("repository serialized credentials")
	}
}
