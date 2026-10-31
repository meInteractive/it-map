// Package apifysettings проверяет и хранит административные настройки Apify.
package apifysettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var ErrInput = errors.New("некорректные настройки Apify")
var ErrDisabled = errors.New("Apify выключен или API-токен не настроен")

type KeyInput struct {
	Profile, APIKey string
	Revision        int64
}
type SettingsInput struct {
	Enabled, PaidAllowed, Confirmed bool
	Revision                        int64
}

var keyPattern = regexp.MustCompile(`^apify_api_[A-Za-z0-9_-]{12,512}$`)

func inputObject(body []byte, allowed ...string) (map[string]json.RawMessage, error) {
	if len(body) > 4096 {
		return nil, ErrInput
	}
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, ErrInput
	}
	valid := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		valid[k] = true
	}
	for k := range fields {
		if !valid[k] {
			return nil, ErrInput
		}
	}
	return fields, nil
}

func revision(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || raw[0] == '"' || string(raw) == "null" {
		return 0, ErrInput
	}
	n, err := strconv.ParseFloat(string(raw), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) || n < -9007199254740991 || n > 9007199254740991 || math.Trunc(n) != n {
		return 0, ErrInput
	}
	return int64(n), nil
}

func ParseKey(body []byte) (KeyInput, error) {
	var out KeyInput
	f, err := inputObject(body, "profile", "apiKey", "revision")
	if err != nil {
		return out, err
	}
	if json.Unmarshal(f["profile"], &out.Profile) != nil || (out.Profile != "free" && out.Profile != "starter") || json.Unmarshal(f["apiKey"], &out.APIKey) != nil {
		return KeyInput{}, ErrInput
	}
	out.APIKey = strings.TrimFunc(out.APIKey, func(r rune) bool {
		return r == '\ufeff' || strings.ContainsRune("\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000", r)
	})
	if !keyPattern.MatchString(out.APIKey) {
		return KeyInput{}, ErrInput
	}
	out.Revision, err = revision(f["revision"])
	if err != nil {
		return KeyInput{}, err
	}
	return out, nil
}

func ParseSettings(body []byte) (SettingsInput, error) {
	var out SettingsInput
	f, err := inputObject(body, "enabled", "paidAllowed", "confirmed", "revision")
	if err != nil {
		return out, err
	}
	if len(f["enabled"]) == 0 || string(f["enabled"]) == "null" || json.Unmarshal(f["enabled"], &out.Enabled) != nil || len(f["paidAllowed"]) == 0 || string(f["paidAllowed"]) == "null" || json.Unmarshal(f["paidAllowed"], &out.PaidAllowed) != nil {
		return SettingsInput{}, ErrInput
	}
	if raw, ok := f["confirmed"]; ok && (string(raw) == "null" || json.Unmarshal(raw, &out.Confirmed) != nil) {
		return SettingsInput{}, ErrInput
	}
	out.Revision, err = revision(f["revision"])
	if err != nil {
		return SettingsInput{}, err
	}
	return out, nil
}

func ValidateTransition(in SettingsInput, freeConfigured, starterConfigured, wasPaidAllowed bool) error {
	if in.Enabled && !freeConfigured && !(in.PaidAllowed && starterConfigured) {
		return ErrDisabled
	}
	if in.PaidAllowed && (!starterConfigured || (!wasPaidAllowed && !in.Confirmed)) {
		return ErrInput
	}
	return nil
}

type StoredSettings struct {
	Enabled, PaidAllowed                                       bool
	Revision                                                   int64
	FreeEncrypted, FreeSuffix, StarterEncrypted, StarterSuffix string
}
type Limits struct {
	Name       string `json:"name"`
	PriceUSD   int    `json:"priceUsd"`
	CreditsUSD int    `json:"creditsUsd"`
}
type PublicProfile struct {
	Configured bool   `json:"configured"`
	KeyMask    string `json:"keyMask"`
	Account    any    `json:"account"`
	LastError  string `json:"lastError"`
	Limits     Limits `json:"limits"`
}
type PublicView struct {
	Enabled     bool                     `json:"enabled"`
	PaidAllowed bool                     `json:"paidAllowed"`
	Revision    int64                    `json:"revision"`
	Profiles    map[string]PublicProfile `json:"profiles"`
}

func PublicSettings(s StoredSettings) PublicView {
	profile := func(encrypted, suffix string, limits Limits) PublicProfile {
		p := PublicProfile{Configured: encrypted != "", Limits: limits}
		if suffix != "" {
			p.KeyMask = "••••" + suffix
		}
		return p
	}
	return PublicView{Enabled: s.Enabled, PaidAllowed: s.PaidAllowed, Revision: s.Revision, Profiles: map[string]PublicProfile{
		"free":    profile(s.FreeEncrypted, s.FreeSuffix, Limits{"Free", 0, 5}),
		"starter": profile(s.StarterEncrypted, s.StarterSuffix, Limits{"Starter", 19, 19}),
	}}
}
