package parser

import (
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

type hhQuarantine struct {
	Page, Offset int64
	Code         string
}
type hhListingCursor struct {
	Version                          int
	Revision                         int64
	Operation                        apifysettings.OperationVersion
	SchedulerHash                    string
	Profile                          string
	TitleQuery                       *string
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

func (c hhListingCursor) options() apify.HHOptions {
	order := "publication_time"
	return apify.HHOptions{Page: c.Page, OrderBy: &order, TitleQuery: c.TitleQuery}
}
func advanceHHListingCursor(c hhListingCursor, slice apifyruns.DatasetSlice, count int, prefix string, now int64) (hhListingCursor, error) {
	if c.Version != 1 || count < 0 || count > 10 || c.Offset < 0 || c.Page < 0 || c.Page > 99 || c.Offset+int64(count) > slice.Metadata.ItemCount || c.RunID != "" && c.RunID != slice.Record.RunID || c.DatasetID != "" && c.DatasetID != slice.Record.DatasetID {
		return c, ErrHHListingChanged
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
	case c.PageCount >= 100 && c.Page < 99:
		c.Page++
		c.Offset = 0
		c.PageCount = 0
		c.RunID = ""
		c.DatasetID = ""
		c.Metadata = nil
		c.MetadataObservedAt = 0
		c.RepeatedPrefix = false
		c.Done = false
	case c.PageCount >= 100:
		c.PartialCode = "apify_page_limit"
	case c.Quarantined > 0:
		c.PartialCode = "apify_partial_data"
	}
	return c, nil
}
