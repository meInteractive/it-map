package parser

import (
	"errors"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var errFirecrawlListing = errors.New("Firecrawl listing has no verified vacancy links")
var errGetmatchListingUnavailable = errors.New("Getmatch did not load company vacancies")

// Match visible standalone status text, not translations or sentences about
// another employer. An ordinary page shell is not evidence of an empty list.
var getmatchEmptyLine = regexp.MustCompile(`(?mi)^[ 	#*_>-]*(?:нет (?:открытых |актуальных |доступных )?вакансий|вакансий (?:пока )?нет|no (?:open |current |available )?(?:jobs|vacancies|positions)(?: available| found)?)[.!*_ 	]*$`)
var getmatchUnavailableLine = regexp.MustCompile(`(?mi)^[ 	#*_>-]*не смогли загрузить данные[.!*_ 	]*$`)

var companyListingLink = regexp.MustCompile(`!?\[([^\]\r\n]+)\]\(<?([^\s<>)]+)>?(?:[ \t]+"[^"\r\n]*")?\)`)
var getmatchVacancyPath = regexp.MustCompile(`^/vacancies/[A-Za-z0-9_-]+/?$`)

func firecrawlVacancyPath(source, path string) bool {
	if source == "getmatch" {
		return getmatchVacancyPath.MatchString(path)
	}
	return companyVacancyPath.MatchString(path)
}

var companyVacancyPath = regexp.MustCompile(`(?i)/(vacancy|vacancies|jobs?|positions?|openings?)/[^/]+/?$`)
var companyListingNavigation = regexp.MustCompile(`(?i)^(главная|вакансии|все вакансии|подробнее|откликнуться|home|careers|jobs|apply|learn more)$`)

type companyListingCard struct {
	Title string
	URL   string
}

func firecrawlListingCards(source, markdown, page string) ([]companyListingCard, error) {
	base, err := url.Parse(page)
	if err != nil || base.Hostname() == "" {
		return nil, errFirecrawlListing
	}
	if source == "getmatch" && getmatchUnavailableLine.MatchString(markdown) {
		return nil, errGetmatchListingUnavailable
	}
	pairs := companyListingLink.FindAllStringSubmatch(markdown, 10001)
	if len(pairs) > 10000 {
		return nil, errFirecrawlListing
	}
	titles := make(map[string]string)
	order := make([]string, 0)
	for _, pair := range pairs {
		if strings.HasPrefix(pair[0], "!") || len(pair[1]) > 2000 || len(pair[2]) > 2048 {
			continue
		}
		title := strings.Join(strings.Fields(html.UnescapeString(pair[1])), " ")
		title = strings.Trim(title, "*_ ")
		if title == "" || utf8.RuneCountInString(title) > 500 ||
			companyListingNavigation.MatchString(title) || strings.IndexFunc(title, unicode.IsLetter) < 0 ||
			strings.IndexFunc(title, unicode.IsControl) >= 0 {
			continue
		}
		u, err := base.Parse(html.UnescapeString(pair[2]))
		if err != nil || u.User != nil || u.Port() != "" || !strings.EqualFold(u.Hostname(), base.Hostname()) ||
			(u.Scheme != "https" && u.Scheme != "http") || !firecrawlVacancyPath(source, u.Path) {
			continue
		}
		_, slug := path.Split(strings.TrimRight(u.Path, "/"))
		switch strings.ToLower(slug) {
		case "all", "list", "search", "index", "categories", "teams", "locations", ".", "..":
			continue
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			continue
		}
		for name := range query {
			switch strings.ToLower(name) {
			case "gclid", "fbclid", "yclid", "msclkid":
				query.Del(name)
			default:
				if strings.HasPrefix(strings.ToLower(name), "utm_") {
					query.Del(name)
				}
			}
		}
		u.Fragment, u.RawFragment, u.RawQuery, u.ForceQuery = "", "", query.Encode(), false
		u.Host = strings.ToLower(u.Host)
		canonical := u.String()
		if len(canonical) > 2048 || strings.TrimRight(u.Path, "/") == strings.TrimRight(base.Path, "/") {
			continue
		}
		if previous, seen := titles[canonical]; !seen {
			titles[canonical] = title
			order = append(order, canonical)
		} else if previous != title {
			titles[canonical] = ""
		}
	}
	cards := make([]companyListingCard, 0, min(200, len(order)))
	for _, link := range order {
		if title := titles[link]; title != "" {
			cards = append(cards, companyListingCard{Title: title, URL: link})
			if len(cards) > 200 {
				return nil, errFirecrawlListing
			}
		}
	}
	if len(cards) == 0 {
		if source == "getmatch" && len(order) == 0 && getmatchEmptyLine.MatchString(markdown) {
			return cards, nil
		}
		return nil, errFirecrawlListing
	}
	return cards, nil
}
