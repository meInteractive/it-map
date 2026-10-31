package parser

import (
	"regexp"
	"strings"
)

const (
	fulltextShort   = "Описание слишком короткое или отсутствует."
	fulltextStatus  = "Не подтверждён успешный статус страницы."
	fulltextBlocked = "Страница закрыта, требует входа или не содержит действующую вакансию."
	fulltextSignals = "Не подтверждено содержательное описание конкретной вакансии."
	fulltextListing = "Получен список нескольких вакансий."
)

var fulltextBlockPattern = regexp.MustCompile(`(?i)captcha|verify[^\n\r\x{2028}\x{2029}]{0,30}human|access denied|вакансия[^\n\r\x{2028}\x{2029}]{0,30}(?:закрыта|удалена|в архиве)|job[^\n\r\x{2028}\x{2029}]{0,30}(?:closed|removed|no longer available)|page not found|страница не найдена|(?:^|\n)` + jsStageSpace + `*#?` + jsStageSpace + `*(?:вход|авторизация|sign in|log in)(?:\n|$)`)
var fulltextSignalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)обязанност|(?:чем|что|вам) предстоит|вы будете|responsibilit|what you[^\n\r\x{2028}\x{2029}]ll do|задачи`),
	regexp.MustCompile(`(?i)требован|ожидаем от|мы жд[её]м|навык|опыт|requirements|qualifications`),
	regexp.MustCompile(`(?i)услови|предлагаем|benefits|what we offer|зарплат|salary`),
}
var fulltextListHeading = regexp.MustCompile(`(?im)^` + jsStageSpace + `*#` + jsStageSpace + `*(?:вакансии|открытые позиции|open positions|job openings)` + jsStageSpace + `*$`)
var fulltextHeadings = regexp.MustCompile(`(?m)^#{1,3} `)

func fulltextPrefix(s string, units int) string {
	if units <= 0 {
		return ""
	}
	var prefix strings.Builder
	prefix.Grow(min(len(s), units*3))
	for _, r := range s {
		width := 1
		if r > 0xffff {
			width = 2
		}
		if width > units {
			// Срез внутри UTF-16 пары сохраняет прежнюю замену на U+FFFD.
			prefix.WriteRune('\ufffd')
			break
		}
		prefix.WriteRune(r)
		units -= width
		if units == 0 {
			break
		}
	}
	return prefix.String()
}

func fulltextAtLeastUTF16(s string, units int) bool {
	if units <= 0 {
		return true
	}
	count := 0
	for _, r := range s {
		count++
		if r > 0xffff {
			count++
		}
		if count >= units {
			return true
		}
	}
	return false
}

// CheckFulltextContent переносит только проверки текста и статуса источника.
// URL, принадлежность rabota.ru и сохранение снимка требуют отдельных проверок.
func CheckFulltextContent(markdown string, status int) []string {
	errors := []string{}
	if !fulltextAtLeastUTF16(jsStageTrim(markdown), 250) {
		errors = append(errors, fulltextShort)
	}
	if status < 200 || status >= 300 {
		errors = append(errors, fulltextStatus)
	}
	text := fulltextPrefix(markdown, 12000)
	head := fulltextPrefix(text, 3000)
	if fulltextBlockPattern.MatchString(stageUTF16RegexText(stageLegacyCaseText(head))) {
		errors = append(errors, fulltextBlocked)
	}
	signals := 0
	for _, pattern := range fulltextSignalPatterns {
		if pattern.MatchString(stageUTF16RegexText(stageLegacyCaseText(text))) {
			signals++
		}
	}
	if signals < 2 {
		errors = append(errors, fulltextSignals)
	}
	lines := strings.NewReplacer("\r", "\n", "\u2028", "\n", "\u2029", "\n")
	if fulltextListHeading.MatchString(lines.Replace(stageLegacyCaseText(head))) && len(fulltextHeadings.FindAllStringIndex(lines.Replace(text), -1)) > 3 {
		errors = append(errors, fulltextListing)
	}
	return errors
}
