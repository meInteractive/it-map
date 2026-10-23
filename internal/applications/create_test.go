package applications

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testGeneration = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
const testKey = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

func creationInput() CreateInput {
	return CreateInput{CompanyName: " Example ", Status: "Откликнулся", Comment: "hi <&>", RecruiterEmail: "first@example.com", ApplicationSources: []string{"telegram", "hh", "hh"}, IdempotencyKey: testKey}
}

func TestCreationCanonicalHashAndInput(t *testing.T) {
	in, err := creationInput().normalize()
	if err != nil {
		t.Fatal(err)
	}
	if in.CompanyName != "Example" || strings.Join(in.ApplicationSources, ",") != "hh,telegram" {
		t.Fatal(in)
	}
	value, err := jsJSON([]any{in.CompanyName, in.VacancyID, in.Status, in.Comment, in.RecruiterName, in.RecruiterPhone, in.RecruiterEmail, in.RecruiterContactType, in.ApplicationSources})
	if err != nil {
		t.Fatal(err)
	}
	expected := `["Example",null,"Откликнулся","hi <&>","","","first@example.com","",["hh","telegram"]]`
	if string(value) != expected {
		t.Fatalf("JSON.stringify mismatch: %s", value)
	}
	hash, err := in.intentHash()
	if err != nil || hash != "85fbb31983981280570709640e61e65185f0928951bb4cd93aa5d2a718f586ce" {
		t.Fatalf("hash=%s err=%v", hash, err)
	}

	literal, err := jsJSON([]string{"line\u2028break"})
	if err != nil || strings.Contains(string(literal), `\u2028`) {
		t.Fatalf("JS unicode separator mismatch: %q %v", literal, err)
	}
	swapped := creationInput()
	swapped.ApplicationSources = []string{"hh", "telegram"}
	normalized, _ := swapped.normalize()
	sameHash, _ := normalized.intentHash()
	if hash != sameHash {
		t.Fatal("same intent had different hash")
	}
	for _, modify := range []func(*CreateInput){
		func(i *CreateInput) { i.IdempotencyKey = "not-uuid" },
		func(i *CreateInput) { i.Status = "NotAStatus" },
		func(i *CreateInput) { i.ApplicationSources = []string{"invalid"} },
		func(i *CreateInput) { i.Comment = strings.Repeat("x", 1001) },
	} {
		bad := creationInput()
		modify(&bad)
		if _, err := bad.normalize(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid input; got %v", err)
		}
	}
}

func TestGenerationBoundCreationContext(t *testing.T) {
	repo := Repository{CreationSecret: []byte(strings.Repeat("s", 64))}
	token, err := repo.signature("Demo", testGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if token != "ac1.LG5qC6hODagmJmOy8URlcUsDJoR2ZZwo4Biaux9YtXs" {
		t.Fatalf("Sites HMAC mismatch: %q", token)
	}
	if !strings.HasPrefix(token, "ac1.") || len(token) != 47 {
		t.Fatal(token)
	}
	if err = repo.verifyContext("Demo", testGeneration, token); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		user, generation, token string
		want                    error
	}{
		{"Eugene", testGeneration, token, ErrCreationContextExpired},
		{"Demo", "cccccccc-cccc-4ccc-8ccc-cccccccccccc", token, ErrCreationContextExpired},
		{"Demo", testGeneration, "", ErrCreationContextRequired},
		{"Demo", testGeneration, "ac1.bad", ErrInvalidInput},
		{"Demo", testGeneration, "ac1." + strings.Repeat("!", 43), ErrInvalidInput},
	} {
		if err = repo.verifyContext(tc.user, tc.generation, tc.token); !errors.Is(err, tc.want) {
			t.Fatalf("want %v got %v", tc.want, err)
		}
	}
	encoded := strings.TrimPrefix(token, "ac1.")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		t.Fatal(err)
	}
}
func TestCatalogMembership(t *testing.T) {
	data := []byte(`{"companies":[{"name":"Example"}],"agencies":[{"name":"Agency"}]}`)
	for _, name := range []string{"Example", "Agency", "Missing"} {
		known, err := isKnownForCreation(data, name)
		if err != nil || known != (name != "Missing") {
			t.Fatal(name, known, err)
		}
	}
	if _, err := isKnownForCreation([]byte(`{"companies":[]}`), "Example"); err == nil {
		t.Fatal("incomplete catalog accepted")
	}
}
func TestNoDBCannotIssueContext(t *testing.T) {
	_, err := (Repository{CreationSecret: []byte("s")}).CreationContext(context.Background(), "Demo")
	if !errors.Is(err, ErrGenerationUnavailable) {
		t.Fatal(err)
	}
}
