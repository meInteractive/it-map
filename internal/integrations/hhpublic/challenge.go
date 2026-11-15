package hhpublic

import "strings"

// verificationChallenge распознаёт явные маркеры страницы проверки, а не
// объявляет любой отказ HH временным. Проверка применяется после ParseJob.
func verificationChallenge(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{
		`class="g-recaptcha"`, `class='g-recaptcha'`,
		`class="h-captcha"`, `class='h-captcha'`,
		`class="smartcaptcha"`, `class='smartcaptcha'`,
		`id="smartcaptcha"`, `id='smartcaptcha'`, "cf-chl-",
		"подтвердите, что вы не робот", "verify you are human",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
