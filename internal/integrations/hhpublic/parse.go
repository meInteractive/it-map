// Package hhpublic читает только публичную структурированную карточку HH.
package hhpublic

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Job struct {
	ID          string `json:"vacancy_id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description_text"`
	Company     string `json:"company"`
	DatePosted  string `json:"date_posted,omitempty"`
}

var publicID = regexp.MustCompile(`^[1-9][0-9]{0,11}$`)
var vacancyURLPath = regexp.MustCompile(`^/vacancy/([1-9][0-9]{0,11})/?$`)

func CanonicalURL(value string) (string, string, bool) {
	u, e := url.Parse(value)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" && u.Port() != "443" {
		return "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "hh.ru" && !strings.HasSuffix(host, ".hh.ru") {
		return "", "", false
	}
	match := vacancyURLPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return "", "", false
	}
	return "https://hh.ru/vacancy/" + match[1], match[1], true
}

var comment = regexp.MustCompile(`(?s)<!--[\s\S]*?-->`)
var script = regexp.MustCompile(`(?is)<script\b[^>]*>[\s\S]*?</script\s*>`)
var style = regexp.MustCompile(`(?is)<style\b[^>]*>[\s\S]*?</style\s*>`)
var newlines = regexp.MustCompile(`(?i)</(?:p|div|h[1-6]|ul|ol)>|<br\s*/?\s*>`)
var listItem = regexp.MustCompile(`(?i)<li\b[^>]*>`)
var tags = regexp.MustCompile(`<[^>]*>`)
var entities = regexp.MustCompile(`(?i)&(#x[0-9a-f]+|#[0-9]+|amp|lt|gt|quot|apos|nbsp);`)
var lineSpaces = regexp.MustCompile(`\n[ \t]+`)
var repeatedLines = regexp.MustCompile(`\n{3,}`)

func trim(value string) string {
	return strings.Trim(value, "\u0009\u000a\u000b\u000c\u000d\u0020\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff")
}
func HTMLText(html string) string {
	text := comment.ReplaceAllString(html, "")
	text = script.ReplaceAllString(text, "")
	text = style.ReplaceAllString(text, "")
	text = newlines.ReplaceAllString(text, "\n")
	text = listItem.ReplaceAllString(text, "\n- ")
	text = tags.ReplaceAllString(text, "")
	text = entities.ReplaceAllStringFunc(text, func(entity string) string {
		e := strings.ToLower(entity[1 : len(entity)-1])
		if e[0] == '#' {
			base, number := 10, e[1:]
			if strings.HasPrefix(number, "x") {
				base, number = 16, number[1:]
			}
			n, err := strconv.ParseUint(number, base, 32)
			if err != nil || n == 0 || n > 0x10ffff || n >= 0xd800 && n <= 0xdfff {
				return ""
			}
			return string(rune(n))
		}
		return map[string]string{"amp": "&", "lt": "<", "gt": ">", "quot": "\"", "apos": "'", "nbsp": " "}[e]
	})
	text = lineSpaces.ReplaceAllString(text, "\n")
	text = repeatedLines.ReplaceAllString(text, "\n\n")
	return trim(text)
}

var ldScript = regexp.MustCompile(`(?is)<script\b[^>]*\btype\s*=\s*["']application/ld\+json["'][^>]*>([\s\S]*?)</script\s*>`)

func date(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// ParseJob принимает единственное совпадение JobPosting. Противоречивый JSON-LD
// не угадывается: вызывающий код может использовать проверенный Apify fallback.
func ParseJob(html, id string, now time.Time) *Job {
	if !publicID.MatchString(id) || !utf8.ValidString(html) {
		return nil
	}
	matching := []map[string]any{}
	var visit func(any, int)
	visit = func(value any, depth int) {
		if depth > 64 {
			return
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				visit(item, depth+1)
			}
		case map[string]any:
			if v["@type"] == "JobPosting" {
				identifier, _ := v["identifier"].(map[string]any)
				value := ""
				switch x := identifier["value"].(type) {
				case string:
					value = x
				case float64:
					value = strconv.FormatFloat(x, 'f', -1, 64)
				}
				if value == id {
					matching = append(matching, v)
				}
			}
			if graph, ok := v["@graph"].([]any); ok {
				visit(graph, depth+1)
			}
		}
	}
	for _, match := range ldScript.FindAllStringSubmatch(html, -1) {
		var value any
		if json.Unmarshal([]byte(match[1]), &value) == nil {
			visit(value, 0)
		}
	}
	if len(matching) != 1 {
		return nil
	}
	j := matching[0]
	title, ok := j["title"].(string)
	if !ok {
		return nil
	}
	description, ok := j["description"].(string)
	if !ok {
		return nil
	}
	if expires, ok := j["validThrough"].(string); ok && expires != "" {
		end, valid := date(expires)
		if !valid || end.Before(now) {
			return nil
		}
	} else if j["validThrough"] != nil && j["validThrough"] != "" {
		return nil
	}
	if link, ok := j["url"].(string); ok && link != "" {
		_, actual, valid := CanonicalURL(link)
		if !valid || actual != id {
			return nil
		}
	} else if j["url"] != nil && j["url"] != "" {
		return nil
	}
	out := &Job{ID: id, URL: "https://hh.ru/vacancy/" + id, Title: HTMLText(title), Description: HTMLText(description)}
	if org, ok := j["hiringOrganization"].(map[string]any); ok {
		if name, ok := org["name"].(string); ok {
			out.Company = HTMLText(name)
		}
	}
	if posted, ok := j["datePosted"].(string); ok && len(posted) >= 10 {
		if _, valid := date(posted); valid {
			out.DatePosted = posted[:10]
		}
	}
	return out
}
