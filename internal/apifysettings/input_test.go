package apifysettings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKeyInput(t *testing.T) {
	key := "apify_api_" + strings.Repeat("x", 16)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"free", `{"profile":"free","apiKey":" ` + key + ` ","revision":0}`, true},
		{"starter", `{"profile":"starter","apiKey":"` + key + `","revision":5}`, true},
		{"unknown profile", `{"profile":"other","apiKey":"` + key + `","revision":0}`, false},
		{"bad key", `{"profile":"free","apiKey":"short","revision":0}`, false},
		{"unknown field", `{"profile":"free","apiKey":"` + key + `","revision":0,"owner":"x"}`, false},
		{"missing revision", `{"profile":"free","apiKey":"` + key + `"}`, false},
		{"negative revision reaches CAS", `{"profile":"free","apiKey":"` + key + `","revision":-1}`, true},
		{"BOM whitespace", `{"profile":"free","apiKey":"\ufeff` + key + `\ufeff","revision":0}`, true},
		{"fractional revision", `{"profile":"free","apiKey":"` + key + `","revision":1.5}`, false},
		{"unsafe revision", `{"profile":"free","apiKey":"` + key + `","revision":9007199254740992}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseKey([]byte(tc.body))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.valid && got.APIKey != key {
				t.Fatal("key was not trimmed")
			}
		})
	}
}

func TestSettingsInputAndPaidConsent(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"enabled":true,"paidAllowed":false,"revision":0}`, true},
		{`{"enabled":true,"paidAllowed":true,"confirmed":true,"revision":3}`, true},
		{`{"enabled":"true","paidAllowed":false,"revision":0}`, false},
		{`{"enabled":true,"paidAllowed":false}`, false},
		{`{"enabled":true,"paidAllowed":false,"revision":0,"extra":true}`, false},
		{`null`, false},
	} {
		_, err := ParseSettings([]byte(tc.body))
		if (err == nil) != tc.valid {
			t.Fatalf("input=%s error=%v", tc.body, err)
		}
	}
	input := SettingsInput{Enabled: true, PaidAllowed: true, Revision: 1}
	if ValidateTransition(input, false, false, false) == nil {
		t.Fatal("enabled without key")
	}
	if ValidateTransition(input, true, true, false) == nil {
		t.Fatal("new paid consent missing")
	}
	input.Confirmed = true
	if err := ValidateTransition(input, true, true, false); err != nil {
		t.Fatal(err)
	}
	input.Confirmed = false
	if err := ValidateTransition(input, true, true, true); err != nil {
		t.Fatal(err)
	}
}

func TestPublicSettingsNeverExposeCiphertext(t *testing.T) {
	key := "apify_api_" + strings.Repeat("x", 16)
	out := PublicSettings(StoredSettings{Revision: 2, FreeEncrypted: "CI_CIPHERTEXT", FreeSuffix: "1234", FreeAccountJSON: `{"accountId":"CI123","planName":"free","providerUsedUsd":1.5,"token":"` + key + `"}`, FreeError: "synthetic " + key})
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "CI_CIPHERTEXT") || strings.Contains(string(b), key) || !strings.Contains(string(b), "1234") || !strings.Contains(string(b), "CI123") {
		t.Fatalf("unsafe projection: %s", b)
	}
}
