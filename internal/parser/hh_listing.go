package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
)

type HHListingRuntime interface {
	Execute(context.Context, apifysettings.OperationVersion, string, apify.HHOptions) (apifyruns.Record, error)
	ReadDataset(context.Context, apifysettings.OperationVersion, string, apify.HHOptions, int64) (apifyruns.DatasetSlice, error)
}
type hhCheckedRuntime interface {
	WithCheck(func(context.Context) error) apifyruns.Service
}
type HHListingProgress struct {
	Page, Offset, Candidates, Created, Quarantined int64
	Done                                           bool
	PartialCode                                    string
}
type HHListingExecutor struct {
	Settings apifysettings.Repository
	Runtime  HHListingRuntime
	Owner    string
	Handoff  *CandidateHandoff
}

func hhListingProgress(c hhListingCursor) HHListingProgress {
	return HHListingProgress{c.Page, c.Offset, c.Candidates, c.Created, c.Quarantined, c.Done, c.PartialCode}
}

// Execute обрабатывает одну raw-порцию под уже полученной lease. Он не запускает
// dispatcher и не завершает job/attempt: общий цикл и его учёт относятся к GO-403.
func (e HHListingExecutor) Execute(ctx context.Context, lease *JobLease) (HHListingProgress, error) {
	if lease == nil || e.Runtime == nil || e.Settings.DB == nil || e.Settings.DB != lease.repo.DB || e.Owner == "" {
		return HHListingProgress{}, ErrJobInput
	}
	job := lease.Job()
	if job.Type != "scan_source" || job.SourceID == nil || *job.SourceID != "hh" || job.CompanyID == nil || job.AgencyID != nil || job.CandidateID != nil {
		return HHListingProgress{}, ErrJobInput
	}
	source, err := e.Settings.ReadHHSource(ctx, *job.CompanyID)
	if err != nil {
		return HHListingProgress{}, err
	}
	cfg, err := e.Settings.Read(ctx)
	if err != nil {
		return HHListingProgress{}, err
	}
	if !source.Enabled || !cfg.Enabled {
		return HHListingProgress{}, apifysettings.ErrDisabled
	}
	profile := "free"
	if !cfg.Profiles[profile].Configured {
		profile = "starter"
		if !cfg.PaidAllowed || !cfg.Profiles[profile].Configured {
			return HHListingProgress{}, apifysettings.ErrMissing
		}
	}
	v := apifysettings.OperationVersion{Generation: job.Generation, CompanyID: *job.CompanyID, SourceURL: source.URL, SourceRevision: source.Revision, SettingsRevision: cfg.Revision, Profile: profile}
	var plan hhListingPlan
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error { var err error; plan, err = e.prepareHHListing(ctx, tx, job, v); return err })
	if err != nil {
		return HHListingProgress{}, err
	}
	cur := plan.Cursor
	if cur.Done {
		return hhListingProgress(cur), nil
	}
	workCtx, cancel := context.WithDeadline(ctx, time.UnixMilli(job.LockedUntil))
	defer cancel()

	runtime := e.Runtime
	check := func(active context.Context) error {
		return lease.WithinOwned(active, func(tx *sql.Tx) error {
			_, hash, _, err := e.hhScope(active, tx, job, cur.Operation)
			if err != nil {
				return err
			}
			if hash != cur.SchedulerHash {
				return ErrHHListingChanged
			}
			return nil
		})
	}
	if scoped, ok := runtime.(hhCheckedRuntime); ok {
		runtime = scoped.WithCheck(check)
	}
	if err := check(workCtx); err != nil {
		return hhListingProgress(cur), err
	}
	key := fmt.Sprintf("listing:%s:%d", plan.ID, cur.Page)
	record, err := runtime.Execute(workCtx, cur.Operation, key, cur.options())
	if errors.Is(err, apifyruns.ErrBudget) && cur.Profile == "free" && cfg.PaidAllowed && cfg.Profiles["starter"].Configured {
		// ErrBudget не выдаёт право POST и не создаёт резерв. Выбор разрешённого
		// Starter сохраняется до сети; при неизвестном исходе переход запрещён.
		err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
			old, exists, err := readHHListingCursor(ctx, tx, plan.ID, e.Owner, job)
			if err != nil {
				return err
			}
			if !exists || old.Revision != cur.Revision {
				return ErrHHListingChanged
			}
			cur.Profile = "starter"
			cur.Operation.Profile = "starter"
			cur.Revision++
			if _, _, _, err = e.hhScope(ctx, tx, job, cur.Operation); err != nil {
				return err
			}
			return persistHHListingCursor(ctx, tx, plan.ID, e.Owner, job, cur, false)
		})
		if err != nil {
			return hhListingProgress(cur), err
		}
		if err := check(workCtx); err != nil {
			return hhListingProgress(cur), err
		}
		record, err = runtime.Execute(workCtx, cur.Operation, key, cur.options())
	}
	if err != nil {
		return hhListingProgress(cur), err
	}
	if record.State != "succeeded" && record.State != "failed" {
		return hhListingProgress(cur), nil
	}
	slice, err := runtime.ReadDataset(workCtx, cur.Operation, key, cur.options(), cur.Offset)
	if err != nil {
		return hhListingProgress(cur), err
	}
	if slice.Record.RunID != record.RunID || slice.Record.DatasetID != record.DatasetID {
		return hhListingProgress(cur), apifyruns.ErrDatasetChanged
	}
	var next hhListingCursor
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		current, exists, err := readHHListingCursor(ctx, tx, plan.ID, e.Owner, job)
		if err != nil {
			return err
		}
		if !exists || current.Revision != cur.Revision {
			return ErrHHListingChanged
		}
		name, hash, keys, err := e.hhScope(ctx, tx, job, cur.Operation)
		if err != nil {
			return err
		}
		if hash != cur.SchedulerHash {
			return ErrHHListingChanged
		}
		next = cur
		handoff := e.Handoff
		if handoff == nil {
			handoff = &CandidateHandoff{Planner: SQLCandidateHandoffPlanner{Owner: e.Owner}}
		}
		prefixValues := [][2]json.RawMessage{}
		for i, raw := range slice.Items {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			pair := [2]json.RawMessage{fields["vacancy_id"], fields["url"]}
			for n := range pair {
				if len(pair[n]) == 0 {
					pair[n] = json.RawMessage(`null`)
				}
			}
			prefixValues = append(prefixValues, pair)
			item, err := parseHHListingVacancy(raw, cur.Operation.SourceURL)
			code := "invalid_vacancy"
			if err == nil {
				var id int64
				var created, imported bool
				id, created, imported, err = saveHHListingCandidate(ctx, tx, job, name, cur.Operation.SourceURL, item)
				if err == nil {
					next.Candidates++
					if created {
						next.Created++
					}
					if !imported && len(keys) > 0 {
						shadow := job
						shadow.CandidateID = &id
						if err = handoff.Enqueue(ctx, tx, shadow); err != nil {
							return err
						}
					}
					continue
				}
				if !errors.Is(err, errHHIdentity) {
					return err
				}
				code = "identity_conflict"
			}
			next.Quarantined++
			if len(next.Quarantine) < 20 {
				next.Quarantine = append(append([]hhQuarantine(nil), next.Quarantine...), hhQuarantine{cur.Page, cur.Offset + int64(i), code})
			}
		}
		prefix, err := canonicalHash(prefixValues)
		if err != nil {
			return err
		}
		next, err = advanceHHListingCursor(next, slice, len(slice.Items), prefix, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		next.Revision++
		return persistHHListingCursor(ctx, tx, plan.ID, e.Owner, job, next, false)
	})
	if errors.Is(err, ErrHHListingChanged) {
		// Другой исполнитель мог уже сохранить эту порцию. Повторное чтение не
		// выполняет сеть и не позволяет старому ответу откатить новый cursor.
		var fresh hhListingCursor
		readErr := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
			var exists bool
			var err error
			fresh, exists, err = readHHListingCursor(ctx, tx, plan.ID, e.Owner, job)
			if err != nil {
				return err
			}
			if !exists {
				return ErrHHListingChanged
			}
			_, hash, _, err := e.hhScope(ctx, tx, job, cur.Operation)
			if err != nil {
				return err
			}
			if hash != cur.SchedulerHash || fresh.Revision <= cur.Revision {
				return ErrHHListingChanged
			}
			return nil
		})
		if readErr == nil {
			return hhListingProgress(fresh), nil
		}
	}
	if err != nil {
		return hhListingProgress(cur), err
	}
	return hhListingProgress(next), nil
}
