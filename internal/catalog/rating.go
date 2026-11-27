package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"itmap/internal/backup"
	"itmap/internal/rucollation"
)

type RatingResult struct {
	OK              bool `json:"ok"`
	Companies       int  `json:"companies"`
	Agencies        int  `json:"agencies"`
	AgenciesSkipped int  `json:"agenciesSkipped"`
}

var agencyRatingWeights = []struct {
	field  string
	weight float64
}{
	{"ethics", 1.6}, {"vacancyHonesty", 1.4}, {"feedback", 1.2}, {"vacancyRelevance", 1},
	{"itExpertise", 1}, {"clientKnowledge", .8}, {"processOrganization", .5}, {"candidateSupport", .5},
}

var agencyTechnology = regexp.MustCompile(`(^|[^a-zа-я0-9])(it|ит)([^a-zа-я0-9]|$)|technolog|технолог|digital|цифров|engineering|инженер|development|разработ`)
var agencyManagement = regexp.MustCompile(`head|lead|manager|director|chief|руковод|директор|начальник|лид`)

func agencyHasITManagement(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, role := range []string{
		"engineering manager", "head of engineering", "head of development", "development manager", "technical product", "technical project",
		"cto", "cio", "технический директор", "техдир", "руководитель разработки", "директор по разработке", "руководитель инженер",
		"руководитель it", "директор it", "руководитель ит", "директор ит", "руководитель цифров", "директор по цифров",
	} {
		if strings.Contains(value, role) {
			return true
		}
	}
	return agencyTechnology.MatchString(value) && agencyManagement.MatchString(value)
}

func agencyOptionsHave(values any, options ...string) bool {
	for _, value := range asSlice(values) {
		if slices.Contains(options, strings.ToLower(strings.TrimSpace(asText(value)))) {
			return true
		}
	}
	return false
}

func agencyRelevanceScore(agency storedEntity) float64 {
	var score float64
	switch strings.ToLower(strings.TrimSpace(asText(agency["executiveSearch"]))) {
	case "да":
		score = 5
	case "частично":
		score = 3
	case "":
		if agencyOptionsHave(agency["agencyTypes"], "executive search") {
			score = 5
		}
	}
	specializations := agency["functionalSpecializations"]
	if agencyOptionsHave(agency["agencyTypes"], "it recruitment") ||
		agencyOptionsHave(specializations, "engineering / development", "it / cio", "data / ai", "cybersecurity", "infrastructure / devops") {
		return score + 5
	}
	if agencyOptionsHave(specializations, "product", "project / program / delivery", "digital transformation") && agencyHasITManagement(asText(agency["roles"])) {
		return score + 3
	}
	return score
}

func recalculateAgency(agency storedEntity) bool {
	var reviews float64
	for _, input := range agencyRatingWeights {
		score, ok := agency[input.field].(float64)
		if !ok {
			return false
		}
		reviews += score * input.weight
	}
	reviews = math.Round(reviews*10) / 10
	relevance := agencyRelevanceScore(agency)
	amount, _ := obj(agency["revenue"])["amount"].(float64)
	var revenue float64
	if amount > 0 {
		revenue = 1
		for _, threshold := range []float64{25e6, 50e6, 100e6, 200e6, 400e6, 800e6, 1500e6, 3000e6, 10000e6} {
			if amount >= threshold {
				revenue++
			}
		}
	}
	total := reviews + relevance + revenue
	ethics := agency["ethics"].(float64)
	honesty := agency["vacancyHonesty"].(float64)
	if ethics == 0 || honesty == 0 {
		total = min(total, 50)
	} else if ethics <= 2 || honesty <= 2 {
		total = min(total, 60)
	}
	agency["score"], agency["reviewsScore"], agency["relevanceScore"], agency["revenueScore"] = math.Round(total), reviews, relevance, revenue
	return true
}

// calculateCatalogRating использует общий нормализатор компаний и явную
// формулу агентств, которую раньше применяла кнопка в GPT Sites.
func calculateCatalogRating(data []byte, now time.Time) ([]byte, RatingResult, error) {
	normalized, err := backup.NormalizeStoredCatalog(data)
	if err != nil {
		return nil, RatingResult{}, err
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(normalized, &root); err != nil {
		return nil, RatingResult{}, err
	}
	var source sourceCatalog
	if err = json.Unmarshal(normalized, &source); err != nil {
		return nil, RatingResult{}, err
	}
	result := RatingResult{OK: true, Companies: len(source.Companies)}
	for _, agency := range source.Agencies {
		if recalculateAgency(agency) {
			result.Agencies++
		} else {
			result.AgenciesSkipped++
		}
	}
	if result.Agencies > 0 {
		keys := map[string]rucollation.Key{}
		for _, agency := range source.Agencies {
			name := nameOf(agency)
			keys[name] = rucollation.Keys(utf16.Encode([]rune(name)))
		}
		sort.SliceStable(source.Agencies, func(i, j int) bool {
			a, b := source.Agencies[i], source.Agencies[j]
			for _, field := range []string{"score", "reviewsScore", "relevanceScore", "revenueScore"} {
				left, lok := a[field].(float64)
				right, rok := b[field].(float64)
				if lok != rok {
					return lok
				}
				if left != right {
					return left > right
				}
			}
			return rucollation.Compare(keys[nameOf(a)], keys[nameOf(b)]) < 0
		})
		for i, agency := range source.Agencies {
			agency["rank"] = i + 1
		}
		root["agencies"], err = json.Marshal(source.Agencies)
		if err != nil {
			return nil, RatingResult{}, err
		}
	}
	root["updated"], _ = json.Marshal(now.UTC().Format("2006-01-02"))
	output, err := json.Marshal(root)
	return output, result, err
}

func (r Repository) RecalculateRating(ctx context.Context) (RatingResult, error) {
	if r.DB == nil {
		return RatingResult{}, errors.New("catalog rating storage unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RatingResult{}, err
	}
	defer tx.Rollback()
	// Сохраняем порядок блокировок импорта каталога и восстановления бэкапа.
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return RatingResult{}, err
	}
	var data []byte
	if err = tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1 FOR UPDATE").Scan(&data); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RatingResult{}, err
	}
	updated, result, err := calculateCatalogRating(data, time.Now())
	if err != nil {
		return RatingResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_catalog(id,data_json,updated_at) VALUES(1,?,CURRENT_TIMESTAMP)
		ON DUPLICATE KEY UPDATE data_json=VALUES(data_json),updated_at=CURRENT_TIMESTAMP`, updated); err != nil {
		return RatingResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return RatingResult{}, err
	}
	return result, nil
}
