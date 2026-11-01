package parser

import (
	"regexp"
	"strings"

	whatwg "github.com/nlnwa/whatwg-url/url"
	"golang.org/x/text/unicode/norm"
)

var (
	importMarkdownLink = regexp.MustCompile(`\[([^\]]+)\]\(https?://[^\s)]+\)`)
	importLink         = regexp.MustCompile(`https?://[^\s<>]+`)
	importHTML         = regexp.MustCompile(`<[^>]*>`)
	importMarkup       = regexp.MustCompile("[*_`#>]+")
	importWords        = regexp.MustCompile(`[\p{L}\p{N}]+`)
	importCamel        = regexp.MustCompile(`([a-z])([A-Z])`)
	importLead         = regexp.MustCompile(`team\s*lead`)
	importCross        = regexp.MustCompile(`кросс[ -]?функциональ`)
	importWrapper      = regexp.MustCompile(`^(компания|дата публикации|опубликовано|зарплата|место работы|локация|формат|график)\s*:`)
	importDate         = regexp.MustCompile(`^\d{1,2}\s+(января|февраля|марта|апреля|мая|июня|июля|августа|сентября|октября|ноября|декабря)[A-Za-z0-9_]`)
)
var importTasks = map[string]bool{"основные задачи": true, "задачи": true, "обязанности": true, "чем предстоит заниматься": true, "что предстоит делать": true, "чем заниматься": true, "вам предстоит": true, "тебе предстоит": true, "что нужно делать": true, "предстоит заниматься": true}
var importRequirements = map[string]bool{"основные требования": true, "требования": true, "требования к кандидату": true, "требования к соискателю": true, "мы ожидаем": true, "мы ждем": true, "что мы ждем": true, "что мы ожидаем": true, "что мы ждем от вас": true, "что мы ожидаем от вас": true, "мы ожидаем от вас": true, "мы ждем от вас": true, "для нас важно": true, "наши ожидания": true, "ожидания": true, "необходимые навыки": true, "что нужно знать": true, "наш кандидат": true}
var importOther = map[string]bool{"условия": true, "мы предлагаем": true, "что мы предлагаем": true, "будет плюсом": true, "будет преимуществом": true, "о компании": true, "о команде": true, "о вакансии": true, "почему мы": true, "что предлагаем": true, "преимущества": true, "бонусы": true}

func importPlain(s string) string {
	s = norm.NFKC.String(s)
	s = importMarkdownLink.ReplaceAllString(s, "$1")
	s = importLink.ReplaceAllString(s, " ")
	s = importHTML.ReplaceAllString(s, " ")
	s = importMarkup.ReplaceAllString(s, " ")
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "ё", "е")
}
func importTokens(s string) []string {
	s = importPlain(s)
	s = importLead.ReplaceAllString(s, "team lead")
	s = importCross.ReplaceAllString(s, "кроссфункциональ")
	s = strings.ReplaceAll(s, "c++", "cplusplus")
	s = strings.ReplaceAll(s, "c#", "csharp")
	return importWords.FindAllString(s, -1)
}
func importNormalized(s string) string { return strings.Join(importTokens(s), " ") }
func importSet(tokens []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range tokens {
		out[v] = true
	}
	return out
}
func importCommon(a, b map[string]bool) int {
	n := 0
	for x := range a {
		if b[x] {
			n++
		}
	}
	return n
}
func importMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func importTrigrams(words []string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i+2 < len(words); i++ {
		out[strings.Join(words[i:i+3], " ")] = true
	}
	return out
}
func importClose(a, b string, words, distinct int, dice, containment, shingles float64) bool {
	aw, bw := strings.Fields(a), strings.Fields(b)
	as, bs := importSet(aw), importSet(bw)
	if importMin(len(aw), len(bw)) < words || importMin(len(as), len(bs)) < distinct {
		return false
	}
	common := float64(importCommon(as, bs))
	if 2*common/float64(len(as)+len(bs)) < dice || common/float64(importMin(len(as), len(bs))) < containment {
		return false
	}
	ag, bg := importTrigrams(aw), importTrigrams(bw)
	if importMin(len(ag), len(bg)) == 0 {
		return false
	}
	return float64(importCommon(ag, bg))/float64(importMin(len(ag), len(bg))) >= shingles
}
func importSections(text, title string) map[string]string {
	result := map[string][]string{"body": {}, "tasks": {}, "requirements": {}}
	section := ""
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(importPlain(raw))
		if line == "" {
			continue
		}
		heading := line
		if strings.HasSuffix(heading, ":") || strings.HasSuffix(heading, "?") {
			heading = strings.TrimSpace(heading[:len(heading)-1])
		}
		if importTasks[heading] {
			section = "tasks"
			continue
		}
		if importRequirements[heading] {
			section = "requirements"
			continue
		}
		if importOther[heading] {
			section = ""
			continue
		}
		if importWrapper.MatchString(line) || importDate.MatchString(line) || importNormalized(line) == importNormalized(title) {
			continue
		}
		result["body"] = append(result["body"], line)
		if section != "" {
			result[section] = append(result[section], line)
		}
	}
	out := map[string]string{}
	for k, v := range result {
		out[k] = importNormalized(strings.Join(v, "\n"))
	}
	return out
}
func importHHPriorityText(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	s = importMarkdownLink.ReplaceAllString(s, "$1")
	s = importLink.ReplaceAllString(s, " ")
	s = importMarkup.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// importSameContent воспроизводит сравнение обязанностей и требований старого
// импорта для пары HH и другой публикации той же компании.
func importSameContent(mainURL, mainTitle, mainBody, newURL, newTitle, newBody string) bool {
	if importHHIdentity(mainURL) == "" && importHHIdentity(newURL) != "" {
		body := importHHPriorityText(newBody)
		if utf16Length(body) >= 250 && importHHPriorityText(mainTitle) == importHHPriorityText(newTitle) && importHHPriorityText(mainBody) == body {
			return true
		}
	}
	a, ae := whatwg.Parse(mainURL)
	b, be := whatwg.Parse(newURL)
	if ae != nil || be != nil {
		return false
	}
	if strings.TrimPrefix(a.Hostname(), "www.") == strings.TrimPrefix(b.Hostname(), "www.") {
		return false
	}
	at := importSet(importTokens(importCamel.ReplaceAllString(mainTitle, "$1 $2")))
	bt := importSet(importTokens(importCamel.ReplaceAllString(newTitle, "$1 $2")))
	if importMin(len(at), len(bt)) < 2 || float64(importCommon(at, bt))/float64(importMin(len(at), len(bt))) < .8 {
		return false
	}
	levels := map[string]bool{"junior": true, "middle": true, "senior": true, "младший": true, "старший": true}
	al, bl := map[string]bool{}, map[string]bool{}
	for k := range at {
		if levels[k] {
			al[k] = true
		}
	}
	for k := range bt {
		if levels[k] {
			bl[k] = true
		}
	}
	if len(al) > 0 && len(bl) > 0 && importCommon(al, bl) == 0 {
		return false
	}
	x, y := importSections(mainBody, mainTitle), importSections(newBody, newTitle)
	if x["body"] == "" || y["body"] == "" || strings.Contains(strings.ToLower(mainBody+newBody), "truncated for model") {
		return false
	}
	shared := false
	for _, key := range []string{"tasks", "requirements"} {
		if x[key] != "" && y[key] != "" {
			shared = true
			w, d := 35, 22
			if key == "requirements" {
				w, d = 12, 10
			}
			if !importClose(x[key], y[key], w, d, .88, .90, .72) {
				return false
			}
		}
	}
	if shared {
		for _, key := range []string{"tasks", "requirements"} {
			if (x[key] == "") != (y[key] == "") {
				w, d := 35, 22
				if key == "requirements" {
					w, d = 12, 10
				}
				v, body := x[key], y["body"]
				if v == "" {
					v, body = y[key], x["body"]
				}
				if !importClose(v, body, w, d, 0, .95, .85) {
					return false
				}
			}
		}
		return true
	}
	return importClose(x["body"], y["body"], 100, 65, .94, .96, .88)
}
