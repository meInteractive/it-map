// Package catalog reads the existing catalog JSON from MariaDB without
// mutating company identity tables or making network calls.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type Match struct {
	Name   string `json:"name"`
	Sector string `json:"sector"`
}

type Repository struct{ DB *sql.DB }

func (r Repository) SearchCompanies(ctx context.Context, query string) ([]Match, error) {
	if r.DB == nil {
		return nil, errors.New("catalog database not configured")
	}
	var data []byte
	if err := r.DB.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id = 1").Scan(&data); err != nil {
		return nil, err
	}
	return MatchesFromJSON(data, query)
}

type office struct {
	Metro   string `json:"metro"`
	Address string `json:"address"`
}
type job struct {
	Title string `json:"title"`
}
type revenue struct {
	Legal  string `json:"legal"`
	Metric string `json:"metric"`
}
type company struct {
	Name    string                     `json:"name"`
	Sector  string                     `json:"sector"`
	Roles   string                     `json:"roles"`
	Offices []office                   `json:"offices"`
	Jobs    []job                      `json:"jobs"`
	Revenue revenue                    `json:"revenue"`
	VPNAI   map[string]json.RawMessage `json:"vpnAi"`
}
type companyCatalog struct {
	Companies []company `json:"companies"`
}

// MatchesFromJSON follows searchCatalogNames from the v316 Worker. It searches
// name, sector, roles, offices, embedded jobs, legal entity, revenue metric and
// vpnAi text. It does not expose the full companies, resumes or CRM records.
func MatchesFromJSON(data []byte, query string) ([]Match, error) {
	var catalog companyCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	matches := make([]Match, 0)
	for _, c := range catalog.Companies {
		var fields []string
		fields = append(fields, c.Name, c.Sector, c.Roles)
		for _, o := range c.Offices {
			fields = append(fields, o.Metro, o.Address)
		}
		for _, j := range c.Jobs {
			fields = append(fields, j.Title)
		}
		fields = append(fields, c.Revenue.Legal, c.Revenue.Metric)
		for _, value := range c.VPNAI {
			var txt string
			if json.Unmarshal(value, &txt) == nil {
				fields = append(fields, txt)
			}
		}
		if strings.Contains(strings.ToLower(strings.Join(fields, " ")), q) {
			matches = append(matches, Match{Name: c.Name, Sector: c.Sector})
		}
	}
	return matches, nil
}
