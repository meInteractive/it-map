package vacancies

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// ErrInvalidVacancyInput marks client-side validation failures. The HTTP
// mutation endpoint must remain disabled until its transactional repository
// and v316 response are fully implemented.
var ErrInvalidVacancyInput = errors.New("invalid vacancy input")

type Input struct {
	CompanyName        string  `json:"companyName"`
	EntityType         string  `json:"entityType"`
	RealCompanyName    string  `json:"realCompanyName"`
	URL                string  `json:"url"`
	Title              string  `json:"title"`
	Role               string  `json:"role"`
	RoleIDs            []int64 `json:"roleIds"`
	Level              string  `json:"level"`
	Relevance          *int64  `json:"relevance"`
	PublishedAt        string  `json:"publishedAt"`
	LoadedAt           string  `json:"loadedAt"`
	DescriptionMD      string  `json:"descriptionMd"`
	CoverLetterSubject string  `json:"coverLetterSubject"`
	CoverLetterMD      string  `json:"coverLetterMd"`
	TelegramChannelID  *int64  `json:"telegramChannelId"`
	TelegramMessageURL string  `json:"telegramMessageUrl"`
	TelegramContact    string  `json:"telegramContact"`
	SalaryFrom         *int64  `json:"salaryFrom"`
	SalaryTo           *int64  `json:"salaryTo"`
}

func jsLength(s string) int { return len(utf16.Encode([]rune(s))) }
func validDate(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// NormalizeInput reproduces the v316 vacancyInput business constraints without
// opening a DB transaction. Admin authorization, URL uniqueness, role existence,
// optimistic revision, links and provenance will be enforced by the writer.
func NormalizeInput(in Input, allowEmptyFields bool) (Input, error) {
	invalid := func() (Input, error) { return in, ErrInvalidVacancyInput }
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	in.EntityType = strings.ToLower(strings.TrimSpace(in.EntityType))
	if in.EntityType != "agency" && in.EntityType != "channel" {
		in.EntityType = "company"
	}
	in.RealCompanyName = strings.TrimSpace(in.RealCompanyName)
	in.URL = strings.TrimSpace(in.URL)
	in.Title = strings.TrimSpace(in.Title)
	in.Role = strings.TrimSpace(in.Role)
	level := strings.TrimSpace(in.Level)
	in.Level = ""
	for _, allowed := range []string{"Senior", "Middle", "Lead", "Head"} {
		if strings.EqualFold(level, allowed) {
			in.Level = allowed
			break
		}
	}
	in.PublishedAt = strings.TrimSpace(in.PublishedAt)
	in.LoadedAt = strings.TrimSpace(in.LoadedAt)
	in.TelegramMessageURL = strings.TrimSpace(in.TelegramMessageURL)
	in.TelegramContact = strings.TrimSpace(in.TelegramContact)
	if in.EntityType == "channel" && in.CompanyName == "" && in.TelegramChannelID != nil && *in.TelegramChannelID > 0 {
		in.CompanyName = "__telegram_channel__:" + strconv.FormatInt(*in.TelegramChannelID, 10)
	}
	if in.EntityType == "company" {
		in.RealCompanyName = ""
	}
	parsed, err := url.Parse(in.URL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || jsLength(in.URL) > 2048 {
		return invalid()
	}
	// JS URL.href canonicalizes empty paths to '/'. The Go url.URL.String
	// method does not, so normalize that case explicitly for deduplication.
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	in.URL = parsed.String()
	if (!allowEmptyFields && (in.Title == "" || in.Level == "" || strings.TrimSpace(in.DescriptionMD) == "")) ||
		jsLength(in.Title) > 300 || jsLength(in.RealCompanyName) > 300 ||
		(level != "" && in.Level == "") ||
		!validDate(in.PublishedAt) || !validDate(in.LoadedAt) ||
		jsLength(in.DescriptionMD) > 100000 || jsLength(in.CoverLetterSubject) > 512 || jsLength(in.CoverLetterMD) > 2000 ||
		jsLength(in.TelegramContact) > 256 {
		return invalid()
	}
	if in.EntityType == "channel" && (in.TelegramChannelID == nil || *in.TelegramChannelID <= 0) {
		return invalid()
	}
	if in.TelegramChannelID != nil && (*in.TelegramChannelID <= 0 || in.TelegramMessageURL == "") {
		return invalid()
	}
	if in.Relevance != nil && (*in.Relevance < 0 || *in.Relevance > 100) {
		return invalid()
	}
	if in.SalaryFrom != nil && *in.SalaryFrom < 0 || in.SalaryTo != nil && *in.SalaryTo < 0 {
		return invalid()
	}
	if in.SalaryFrom != nil && in.SalaryTo != nil && *in.SalaryFrom > *in.SalaryTo {
		return invalid()
	}
	seen := map[int64]bool{}
	normalizedRoles := make([]int64, 0, len(in.RoleIDs))
	for _, id := range in.RoleIDs {
		if id <= 0 {
			return invalid()
		}
		if !seen[id] {
			normalizedRoles = append(normalizedRoles, id)
			seen[id] = true
		}
	}
	if in.RoleIDs != nil {
		in.RoleIDs = normalizedRoles
	}
	return in, nil
}
