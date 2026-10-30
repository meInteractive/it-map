package parser

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrSnapshotInvalid = errors.New("vacancy raw snapshot is missing or corrupted")

type RawSnapshotPart struct {
	Index    int
	Markdown string
}

// Собирает части в сохранённом порядке; повреждённый снимок не усекать и не отправлять модели.
func AssembleRawSnapshot(inline *string, parts []RawSnapshotPart, expectedHash string) (string, error) {
	var raw string
	if inline != nil {
		raw = *inline
	} else {
		var b strings.Builder
		for i, p := range parts {
			if p.Index != i || !utf8.ValidString(p.Markdown) {
				return "", ErrSnapshotInvalid
			}
			b.WriteString(p.Markdown)
		}
		if len(parts) == 0 {
			return "", ErrSnapshotInvalid
		}
		raw = b.String()
	}
	if !utf8.ValidString(raw) || len(expectedHash) != 64 || stageHash(raw) != expectedHash {
		return "", ErrSnapshotInvalid
	}
	return raw, nil
}

var rabotaEnd = regexp.MustCompile(`(?i)#{2,3}` + jsStageSpace + `*Смотрите также|Задайте вопрос работодателю|Напишите работодателю свой телефон`)
var rabotaEvidence = regexp.MustCompile(`(?i)обязанност|(?:чем|что|вам) предстоит|вы будете|responsibilit|what you[^\n\r\x{2028}\x{2029}]ll do|задачи|отвечать за|в зону ответственности|развива(?:ть|ем) продукт|управл(?:ять|ение) продукт`)
var rabotaLinks = regexp.MustCompile(`https?://[^\x09-\x0d \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+`)

func RabotaBodyPresent(markdown string) bool {
	// Replacements retain byte positions of the original only after splitting.
	// Find the boundary against a separate copy with equal-width substitutions.
	safe := strings.ReplaceAll(markdown, "ſ", "ø")
	safe = strings.ReplaceAll(safe, "K", "※")
	if loc := rabotaEnd.FindStringIndex(safe); loc != nil {
		markdown = markdown[:loc[0]]
	}
	return rabotaEvidence.MatchString(stageLegacyCaseText(markdown)) && utf16Length(jsStageTrim(rabotaLinks.ReplaceAllString(markdown, ""))) >= 250
}
