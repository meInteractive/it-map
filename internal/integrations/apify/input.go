package apify

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const Actor = "shahidirfan~hh-ru-job-scraper"
const RunCapUSD = 0.25
const ReadReserveUSD = 0.01
const maxSafeInteger = 9007199254740991

var vacancyPath = regexp.MustCompile(`^/vacancy/([0-9]+)/?$`)
var employerPath = regexp.MustCompile(`^/employer/([0-9]+)/?$`)
var digits = regexp.MustCompile(`^[0-9]+$`)
var providerID = regexp.MustCompile(`^[A-Za-z0-9]+$`)

type HHOptions struct {
	Page       int64
	Detail     bool
	OrderBy    *string
	TitleQuery *string
}
type StartInput struct {
	StartURL           string `json:"startUrl"`
	ResultsWanted      int    `json:"results_wanted"`
	MaxPages           int    `json:"max_pages"`
	APIOnly            bool   `json:"api_only"`
	IncludeRawData     bool   `json:"include_raw_data"`
	ProxyConfiguration struct {
		UseApifyProxy bool `json:"useApifyProxy"`
	} `json:"proxyConfiguration"`
}
type pair struct{ key, value string }

func queryPairs(raw string) ([]pair, bool) {
	pairs := []pair{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		bits := strings.SplitN(part, "=", 2)
		key, err := url.QueryUnescape(bits[0])
		if err != nil {
			return nil, false
		}
		value := ""
		if len(bits) == 2 {
			value, err = url.QueryUnescape(bits[1])
			if err != nil {
				return nil, false
			}
		}
		pairs = append(pairs, pair{key, value})
	}
	return pairs, true
}
func querySet(pairs []pair, key, value string) []pair {
	next := []pair{}
	found := false
	for _, p := range pairs {
		if p.key == key {
			if !found {
				next = append(next, pair{key, value})
				found = true
			}
		} else {
			next = append(next, p)
		}
	}
	if !found {
		next = append(next, pair{key, value})
	}
	return next
}
func queryGet(pairs []pair, key string) (string, int) {
	first := ""
	count := 0
	for _, p := range pairs {
		if p.key == key {
			if count == 0 {
				first = p.value
			}
			count++
		}
	}
	return first, count
}
func formEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value), "~", "%7E"), "%2A", "*")
}
func queryString(pairs []pair) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, formEscape(p.key)+"="+formEscape(p.value))
	}
	return strings.Join(parts, "&")
}

// URL serialization uses the HTTPS special-query encode set; vacancy URLs do
// not pass through URLSearchParams, so spaces stay %20 instead of becoming +.
func specialQuery(raw string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for _, c := range []byte(raw) {
		if c <= 0x20 || c >= 0x7f || c == '"' || c == 0x27 || c == '<' || c == '>' {
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&15])
		} else {
			out.WriteByte(c)
		}
	}
	return out.String()
}

// HHInput covers canonical public HTTPS HH source URLs. It preserves query
// insertion/set semantics, since the serialized input is part of legacy hashes.
func HHInput(source string, options HHOptions) (StartInput, error) {
	if !utf8.ValidString(source) {
		return StartInput{}, apiError("apify_scope", 400)
	}
	if options.Page < 0 || options.Page > maxSafeInteger {
		return StartInput{}, apiError("apify_data", 400)
	}
	if options.OrderBy != nil && *options.OrderBy != "publication_time" {
		return StartInput{}, apiError("apify_data", 400)
	}
	if options.TitleQuery != nil {
		q := *options.TitleQuery
		if !utf8.ValidString(q) || !strings.HasPrefix(q, "NAME:(") || len(utf16.Encode([]rune(q))) > 2000 {
			return StartInput{}, apiError("apify_data", 400)
		}
		for _, r := range q {
			if r < 32 {
				return StartInput{}, apiError("apify_data", 400)
			}
		}
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return StartInput{}, apiError("apify_scope", 400)
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "hh.ru" && !strings.HasSuffix(host, ".hh.ru") {
		return StartInput{}, apiError("apify_scope", 400)
	}
	parsed.Host = host
	path := parsed.EscapedPath()
	vacancy := vacancyPath.FindStringSubmatch(path)
	employer := employerPath.FindStringSubmatch(path)
	if options.Detail && vacancy == nil {
		return StartInput{}, apiError("apify_scope", 400)
	}
	if vacancy == nil {
		pairs, valid := queryPairs(parsed.RawQuery)
		if !valid {
			return StartInput{}, apiError("apify_scope", 400)
		}
		employerID, count := queryGet(pairs, "employer_id")
		if employer != nil {
			employerID = employer[1]
		}
		if !digits.MatchString(employerID) || (employer == nil && (count != 1 || path != "/search/vacancy")) {
			return StartInput{}, apiError("apify_scope", 400)
		}
		if employer != nil {
			parsed = &url.URL{Scheme: "https", Host: "hh.ru", Path: "/search/vacancy"}
			pairs = []pair{{"employer_id", employerID}}
		}
		parsed.Host = "hh.ru"
		pairs = querySet(pairs, "page", strconv.FormatInt(options.Page, 10))
		pairs = querySet(pairs, "per_page", "100")
		_, orders := queryGet(pairs, "order_by")
		if options.OrderBy != nil && orders == 0 {
			pairs = querySet(pairs, "order_by", *options.OrderBy)
		}
		if options.TitleQuery != nil && *options.TitleQuery != "" {
			old, _ := queryGet(pairs, "text")
			q := *options.TitleQuery
			if old != "" {
				q = "(" + old + ") AND (" + q + ")"
			}
			pairs = querySet(pairs, "text", q)
		}
		parsed.RawQuery = queryString(pairs)
	}
	if vacancy != nil {
		parsed.RawQuery = specialQuery(parsed.RawQuery)
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	result := StartInput{StartURL: parsed.String(), ResultsWanted: 100, MaxPages: 1, APIOnly: vacancy == nil}
	if vacancy != nil {
		result.ResultsWanted = 1
	}
	result.ProxyConfiguration.UseApifyProxy = true
	return result, nil
}
