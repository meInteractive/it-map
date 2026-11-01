package apifysettings

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

var configClass = regexp.MustCompile(`^[-a-zA-Z0-9_]{0,80}$`)
var configPath = regexp.MustCompile(`^/[A-Za-z0-9/_-]+/$`)
var configQuery = regexp.MustCompile(`^[-a-zA-Z0-9_]{1,40}$`)
var configLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var configIP = regexp.MustCompile(`^\d+(?:\.\d+){3}$`)

func configTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	}
	return true
}

// Нормализация повторяет validateListingConfig старого сервера, включая значения по умолчанию.
func normalizeListingConfig(raw []byte) ([]byte, error) {
	var f map[string]any
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	if json.Unmarshal(raw, &f) != nil || f == nil {
		return nil, ErrInput
	}
	allowed := map[string]bool{"adapter": true, "html": true, "titleClass": true, "detailPathPrefix": true, "allowedHosts": true, "pagination": true, "useEnhancedProxy": true}
	for k := range f {
		if !allowed[k] {
			return nil, ErrInput
		}
	}
	for k, v := range map[string]any{"adapter": "generic", "titleClass": "", "detailPathPrefix": "", "allowedHosts": []any{}} {
		if !configTruthy(f[k]) {
			f[k] = v
		}
	}
	for _, k := range []string{"html", "useEnhancedProxy"} {
		if f[k] == nil {
			f[k] = false
		}
		if _, ok := f[k].(bool); !ok {
			return nil, ErrInput
		}
	}
	adapter, ok := f["adapter"].(string)
	if !ok || !map[string]bool{"generic": true, "habr": true, "getmatch": true, "rabota": true, "selectel": true, "yoomoney": true}[adapter] {
		return nil, ErrInput
	}
	title, ok := f["titleClass"].(string)
	if !ok || !configClass.MatchString(title) || (title != "" && !f["html"].(bool)) {
		return nil, ErrInput
	}
	path, ok := f["detailPathPrefix"].(string)
	if !ok || len(path) > 200 || (path != "" && (!configPath.MatchString(path) || path == "/")) {
		return nil, ErrInput
	}
	hosts, ok := f["allowedHosts"].([]any)
	if !ok || len(hosts) > 3 {
		return nil, ErrInput
	}
	for _, v := range hosts {
		h, ok := v.(string)
		if !ok || h != strings.ToLower(h) {
			return nil, ErrInput
		}
		u, err := url.Parse("https://" + h)
		if err != nil || u.Hostname() != h || u.Host != h {
			return nil, ErrInput
		}
		checked := strings.TrimSuffix(h, ".")
		if len(checked) > 253 || !strings.Contains(checked, ".") || configIP.MatchString(checked) || checked == "metadata" {
			return nil, ErrInput
		}
		labels := strings.Split(checked, ".")
		for _, label := range labels {
			if !configLabel.MatchString(label) {
				return nil, ErrInput
			}
		}
		if map[string]bool{"localhost": true, "local": true, "internal": true, "lan": true, "test": true, "invalid": true, "example": true, "onion": true}[labels[len(labels)-1]] {
			return nil, ErrInput
		}
	}
	if f["pagination"] == nil {
		f["pagination"] = nil
	} else {
		p, ok := f["pagination"].(map[string]any)
		if !ok || len(p) != 1 {
			return nil, ErrInput
		}
		q, ok := p["queryParam"].(string)
		if !ok || !configQuery.MatchString(q) {
			return nil, ErrInput
		}
	}
	return json.Marshal(f)
}
