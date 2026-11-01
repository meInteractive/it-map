package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"itmap/internal/integrations/hhpublic"
)

type HHPublicFetcher interface {
	Fetch(context.Context, string) (*hhpublic.Job, error)
}
type HHDetailRuntime interface {
	HHListingRuntime
	ResolveProfile(context.Context, apifysettings.OperationVersion, string, apify.HHOptions) (string, error)
}
type HHFullTextResult struct {
	SnapshotID      string
	Reused, Pending bool
}
type HHFullTextExecutor struct {
	Settings apifysettings.Repository
	Runtime  HHDetailRuntime
	Public   HHPublicFetcher
	Owner    string
	Handoff  *CandidateHandoff
}

func sameHHDetailURL(a, b string) bool {
	x, err := apify.HHInput(a, apify.HHOptions{Detail: true})
	if err != nil {
		return false
	}
	y, err := apify.HHInput(b, apify.HHOptions{Detail: true})
	if err != nil {
		return false
	}
	u, err := url.Parse(x.StartURL)
	if err != nil {
		return false
	}
	v, err := url.Parse(y.StartURL)
	if err != nil {
		return false
	}
	return strings.TrimSuffix(u.Path, "/") == strings.TrimSuffix(v.Path, "/")
}
func (e HHFullTextExecutor) handoffHHDetail(ctx context.Context, tx *sql.Tx, j Job) error {
	h := e.Handoff
	if h == nil {
		h = &CandidateHandoff{Planner: SQLCandidateHandoffPlanner{Owner: e.Owner}}
	}
	return h.Enqueue(ctx, tx, j)
}

// Execute использует текущую acquired lease и один проверенный снимок. Worker и
// завершение общего job/attempt остаются ответственностью диспетчера GO-403.
func (e HHFullTextExecutor) Execute(ctx context.Context, lease *JobLease) (result HHFullTextResult, err error) {
	if lease == nil || e.Settings.DB == nil || e.Settings.DB != lease.repo.DB || e.Owner == "" {
		return result, ErrJobInput
	}
	job := lease.Job()
	var plan hhFullTextPlan
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		var err error
		plan, err = e.readHHFullTextPlan(ctx, tx, job)
		if err != nil {
			return err
		}
		if plan.Cached == nil {
			plan.Token, plan.Slot, plan.Started, err = acquireDetailLeases(ctx, tx, job, *job.CandidateID, false)
		}
		return err
	})
	if err != nil {
		return result, err
	}
	if plan.Cached != nil {
		err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
			if err := e.validateHHFullTextPlan(ctx, tx, job, plan); err != nil {
				return err
			}
			return e.handoffHHDetail(ctx, tx, job)
		})
		if err != nil {
			return result, err
		}
		return HHFullTextResult{SnapshotID: plan.Cached.ID, Reused: true}, nil
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		release := lease.WithinOwned(cleanup, func(tx *sql.Tx) error {
			return releaseOwnedDetail(cleanup, tx, job, *job.CandidateID, plan.Slot, plan.Token, false)
		})
		err = errors.Join(err, release)
	}()
	bounded, cancel := context.WithDeadline(ctx, time.UnixMilli(job.LockedUntil))
	defer cancel()
	public := e.Public
	if public == nil {
		public = hhpublic.New()
	}
	publicJob, err := public.Fetch(bounded, plan.URL)
	if err != nil {
		return result, err
	}
	var document hhDetailDocument
	provider, profile, runID := "hh-public", "", ""
	valid := false
	if publicJob != nil {
		raw, _ := json.Marshal(publicJob)
		document, err = parseHHDetail(raw, plan.URL)
		valid = err == nil
	}
	var version *apifysettings.OperationVersion
	if !valid {
		runtime := e.Runtime
		if runtime == nil {
			runtime = apifyruns.Service{Repository: apifyruns.Repository{DB: e.Settings.DB, Settings: e.Settings}}
		}
		cfg, err := e.Settings.Read(bounded)
		if err != nil {
			return result, err
		}
		if !cfg.Enabled {
			return result, apifysettings.ErrDisabled
		}
		v := apifysettings.OperationVersion{Generation: job.Generation, CompanyID: *job.CompanyID, SourceURL: plan.Source.URL, SourceRevision: plan.Source.Revision, SettingsRevision: cfg.Revision, Profile: "free", CandidateID: *job.CandidateID, TitleRevision: plan.Title.TitleRevision, VacancyURL: plan.URL}
		old := "missing"
		if plan.LastSnapshotID != "" {
			old = plan.LastSnapshotID
		}
		key := fmt.Sprintf("detail:%d:%d:%s:proxy-v1", v.CandidateID, v.TitleRevision, old)
		options := apify.HHOptions{Detail: true}
		v.Profile, err = runtime.ResolveProfile(bounded, v, key, options)
		if err != nil {
			return result, err
		}
		run, err := runtime.Execute(bounded, v, key, options)
		if errors.Is(err, apifyruns.ErrBudget) && v.Profile == "free" && cfg.PaidAllowed && cfg.Profiles["starter"].Configured {
			v.Profile = "starter"
			run, err = runtime.Execute(bounded, v, key, options)
		}
		if err != nil {
			return result, err
		}
		if run.State != "succeeded" && run.State != "failed" {
			return HHFullTextResult{Pending: true}, ErrHHDetailPending
		}
		provider, profile, runID = "apify", v.Profile, run.RunID
		version = &v
		if run.State == "succeeded" {
			slice, err := runtime.ReadDataset(bounded, v, key, options, 0)
			if err != nil {
				return result, err
			}
			if slice.Record.RunID != run.RunID || slice.Record.DatasetID != run.DatasetID {
				return result, apifyruns.ErrDatasetChanged
			}
			if len(slice.Items) == 1 && slice.Metadata.ItemCount == 1 {
				document, err = parseHHDetail(slice.Items[0], plan.URL)
				valid = err == nil
			}
		}
	}
	err = lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		if err := e.validateHHFullTextPlan(ctx, tx, job, plan); err != nil {
			return err
		}
		if version != nil {
			if _, err := e.Settings.ResolveOperationTx(ctx, tx, *version); err != nil {
				return err
			}
		}
		if !valid {
			return saveHHFetchHistory(ctx, tx, job, plan, "", provider, profile, false)
		}
		var err error
		result, err = saveHHFullTextSnapshot(ctx, tx, job, plan, document, provider, profile, runID)
		if err != nil {
			return err
		}
		return e.handoffHHDetail(ctx, tx, job)
	})
	if err != nil {
		return HHFullTextResult{}, err
	}
	if !valid {
		return HHFullTextResult{}, ErrHHDetailInvalid
	}
	return result, nil
}
