package parser

import (
	"regexp"
	"strings"
	"unicode/utf16"
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
	encoded := utf16.Encode([]rune(s))
	if len(encoded) > units {
		encoded = encoded[:units]
	}
	return string(utf16.Decode(encoded))
}

// CheckFulltextContent переносит только проверки текста и статуса источника.
// URL, принадлежность rabota.ru и сохранение снимка требуют отдельных проверок.
func CheckFulltextContent(markdown string, status int) []string {
	errors := []string{}
	if utf16Length(jsStageTrim(markdown)) < 250 {
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
