package parser

import (
	"encoding/json"
	"errors"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

var errHHDate = errors.New("некорректная дата публикации HH")

func (c hhListingCursor) includesPublication(raw json.RawMessage) (bool, error) {
	if !c.DateFilterApplied || c.VacancyAgeDays == 0 {
		return true, nil
	}
	if c.DateFrom == nil || c.DateTo == nil {
		return false, errHHDate
	}
	from, err := time.Parse(time.RFC3339, *c.DateFrom)
	if err != nil {
		return false, errHHDate
	}
	to, err := time.Parse(time.RFC3339, *c.DateTo)
	if err != nil || !from.Before(to) {
		return false, errHHDate
	}
	var fields struct {
		DatePosted string `json:"date_posted"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return false, errHHDate
	}
	published, err := time.Parse(time.RFC3339Nano, fields.DatePosted)
	if err != nil {
		return false, errHHDate
	}
	return !published.Before(from) && published.Before(to.Add(time.Second)), nil
}

type hhQuarantine struct {
	Page, Offset int64
	Code         string
}
type hhListingCursor struct {
	Version                          int
	Revision                         int64
	Operation                        apifysettings.OperationVersion
	PreviousOperation                *apifysettings.OperationVersion `json:",omitempty"`
	SchedulerHash                    string
	Profile                          string
	TitleQuery                       *string
	DateFrom, DateTo                 *string `json:",omitempty"`
	DateFilterApplied                bool    `json:",omitempty"`
	MoscowFilter                     bool    `json:",omitempty"`
	VacancyAgeDays                   int     `json:",omitempty"`
	LineageVersion                   int     `json:",omitempty"`
	CandidateIDs                     []int64 `json:",omitempty"`
	Page, Offset, PageCount          int64
	RunID, DatasetID                 string
	Metadata                         *apify.DatasetMetadata
	MetadataObservedAt               int64
	SeenPrefixes                     []string
	RepeatedPrefix                   bool
	Done                             bool
	PartialCode                      string
	Candidates, Created, Quarantined int64
	Quarantine                       []hhQuarantine
}

func (c hhListingCursor) validLineage() bool {
	if c.LineageVersion == 0 {
		return len(c.CandidateIDs) == 0
	}
	if c.LineageVersion != 1 || len(c.CandidateIDs) > 10000 {
		return false
	}
	seen := make(map[int64]struct{}, len(c.CandidateIDs))
	for _, id := range c.CandidateIDs {
		if id < 1 || id > maxSafeInteger {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

// Связь записывается только в курсоры, созданные с включённой версией учёта.
// Прежние курсоры нельзя задним числом считать полным набором кандидатов.
func (c *hhListingCursor) rememberCandidate(id int64) error {
	if c.LineageVersion == 0 {
		return nil
	}
	if c.LineageVersion != 1 || id < 1 || id > maxSafeInteger || len(c.CandidateIDs) > 10000 {
		return ErrHHListingChanged
	}
	for _, existing := range c.CandidateIDs {
		if existing == id {
			return nil
		}
	}
	if len(c.CandidateIDs) == 10000 {
		return ErrHHListingChanged
	}
	c.CandidateIDs = append(c.CandidateIDs, id)
	return nil
}

func (c hhListingCursor) options() apify.HHOptions {
	order := "publication_time"
	return apify.HHOptions{Page: c.Page, OrderBy: &order, TitleQuery: c.TitleQuery, DateFrom: c.DateFrom, DateTo: c.DateTo, DateFilterApplied: c.DateFilterApplied, MoscowFilter: c.MoscowFilter}
}

func hhDateWindow(now time.Time, days int) (*string, *string) {
	if days == 0 {
		return nil, nil
	}
	moscow := time.FixedZone("MSK", 3*60*60)
	today := now.In(moscow)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, moscow)
	from := start.AddDate(0, 0, -days).Format(time.RFC3339)
	to := start.Add(-time.Second).Format(time.RFC3339)
	return &from, &to
}
func advanceHHListingCursor(c hhListingCursor, slice apifyruns.DatasetSlice, count int, prefix string, now int64) (hhListingCursor, error) {
	if c.Version != 1 || count < 0 || count > 10 || c.Offset < 0 || c.Page < 0 || c.Page > 99 || c.Offset+int64(count) > slice.Metadata.ItemCount || c.RunID != "" && c.RunID != slice.Record.RunID || c.DatasetID != "" && c.DatasetID != slice.Record.DatasetID {
		return c, ErrHHListingChanged
	}
	if count < 10 && c.Offset+int64(count) != slice.Metadata.ItemCount {
		return c, apifyruns.ErrDatasetChanged
	}
	if c.Metadata != nil && (c.Metadata.ItemCount != slice.Metadata.ItemCount || c.Metadata.ModifiedAt != slice.Metadata.ModifiedAt) {
		return c, apifyruns.ErrDatasetChanged
	}
	if c.Metadata == nil {
		metadata := slice.Metadata
		c.Metadata = &metadata
		c.MetadataObservedAt = now
	}
	c.RunID, c.DatasetID = slice.Record.RunID, slice.Record.DatasetID
	if c.Offset == 0 && count > 0 {
		for _, old := range c.SeenPrefixes {
			if old == prefix {
				c.RepeatedPrefix = true
			}
		}
		if !c.RepeatedPrefix {
			c.SeenPrefixes = append(append([]string(nil), c.SeenPrefixes...), prefix)
		}
	}
	c.Offset += int64(count)
	c.PageCount += int64(count)
	stableAt := c.MetadataObservedAt
	if modified, e := time.Parse(time.RFC3339Nano, slice.Metadata.ModifiedAt); e == nil {
		stableAt = modified.UnixMilli()
	}
	if count >= 10 || c.Offset != slice.Metadata.ItemCount || now-stableAt < 5000 || slice.Record.ChargedUSD == nil {
		return c, nil
	}
	c.Done = true
	switch {
	case slice.Record.State == "failed":
		c.PartialCode = "apify_partial_run"
	case *slice.Record.ChargedUSD >= apifyruns.RunCapUSD-1e-9:
		c.PartialCode = "apify_run_limit"
	case c.RepeatedPrefix:
		c.PartialCode = "apify_pagination_uncertain"
	case c.PageCount >= 100 && !c.MoscowFilter:
		c.PartialCode = "moscow_filter_upgrade"
	case c.PageCount >= 100 && c.Page < 99:
		c.Page++
		c.Offset = 0
		c.PageCount = 0
		c.RunID = ""
		c.DatasetID = ""
		c.Metadata = nil
		c.MetadataObservedAt = 0
		c.PreviousOperation = nil
		c.RepeatedPrefix = false
		c.Done = false
	case c.PageCount >= 100:
		c.PartialCode = "apify_page_limit"
	case c.Quarantined > 0:
		c.PartialCode = "apify_partial_data"
	}
	return c, nil
}
