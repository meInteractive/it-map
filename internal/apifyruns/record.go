package apifyruns

import (
	"encoding/json"
	"errors"
	"itmap/internal/integrations/apify"
	"math"
	"time"
)

var ErrIdentity = errors.New("идентификатор операции Apify изменился")
var ErrUncertain = errors.New("исход запуска Apify неизвестен; повтор отключён")

// Record содержит только разрешённые поля журнала, без ключей и ответа провайдера.
type Record struct {
	ID         string
	State      string
	RunID      string
	DatasetID  string
	AccountID  string
	ChargedUSD *float64
}

func mergeRun(row Record, run apify.ActorRun, now time.Time) (Record, error) {
	state, e := run.State()
	if e != nil || !accountID.MatchString(run.ID) || !accountID.MatchString(run.DefaultDatasetID) || row.RunID != "" && row.RunID != run.ID || row.DatasetID != "" && row.DatasetID != run.DefaultDatasetID {
		return Record{}, ErrIdentity
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(run.Raw, &fields) != nil || fields == nil {
		return Record{}, ErrIdentity
	}
	var id, dataset, status, user string
	if json.Unmarshal(fields["id"], &id) != nil || json.Unmarshal(fields["defaultDatasetId"], &dataset) != nil || json.Unmarshal(fields["status"], &status) != nil || id != run.ID || dataset != run.DefaultDatasetID || status != run.Status {
		return Record{}, ErrIdentity
	}
	if raw := fields["userId"]; len(raw) > 0 && string(raw) != "null" {
		if json.Unmarshal(raw, &user) != nil || user != "" && user != row.AccountID {
			return Record{}, ErrIdentity
		}
	}
	var finished string
	_ = json.Unmarshal(fields["finishedAt"], &finished)
	end, parseErr := time.Parse(time.RFC3339Nano, finished)
	recent := parseErr == nil && now.Sub(end) < 10*time.Second
	var cost float64
	raw := fields["usageTotalUsd"]
	if state != "running" && !recent && len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &cost) == nil && cost >= 0 && !math.IsNaN(cost) && !math.IsInf(cost, 0) {
		if row.ChargedUSD == nil || cost > *row.ChargedUSD {
			row.ChargedUSD = &cost
		}
	}
	if (row.State == "succeeded" || row.State == "failed") && state == "running" {
		state = row.State
	}
	row.State, row.RunID, row.DatasetID = state, run.ID, run.DefaultDatasetID
	return row, nil
}
