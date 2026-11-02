package parser

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
)

var importHHPath = regexp.MustCompile(`^/vacancy/([0-9]+)/?$`)

// importHHIdentity повторяет устойчивый HH-ключ существующего импорта:
// параметры отслеживания не меняют публикацию, номер вакансии сохраняется.
func importHHIdentity(raw string) string {
	u, err := whatwg.Parse(raw)
	if err != nil || u.Scheme() != "https" || u.Username() != "" || u.Password() != "" || u.Port() != "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "hh.ru" && !strings.HasSuffix(host, ".hh.ru") {
		return ""
	}
	match := importHHPath.FindStringSubmatch(u.Pathname())
	if match == nil {
		return ""
	}
	return "hh:" + match[1]
}
