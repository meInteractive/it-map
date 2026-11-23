package parser

import (
	"context"
	"errors"
)

var ErrWorkerUnsupported = errors.New("scheduler worker does not support job type")

type workerListing interface {
	Execute(context.Context, *JobLease) (HHListingProgress, error)
}
type workerDetail interface {
	Execute(context.Context, *JobLease) (HHFullTextResult, error)
}
type workerImport interface {
	Execute(context.Context, *JobLease) (HHImportResult, error)
}
type workerAI interface {
	Execute(context.Context, *JobLease) (AIStageOutput, error)
}

// workerDispatch не создаёт lease и не завершает job: эти операции принадлежат
// ограниченному циклу. AI-исполнитель сам завершает свою attempt и job.
type workerDispatch struct {
	listing          workerListing
	firecrawlListing workerListing
	detail           workerDetail
	firecrawlDetail  workerDetail
	importer         workerImport
	ai               workerAI
}
type workerDispatchResult struct {
	pending  bool
	finished bool
	code     string
}

func (d workerDispatch) execute(ctx context.Context, lease *JobLease) (workerDispatchResult, error) {
	if lease == nil {
		return workerDispatchResult{}, ErrJobInput
	}
	switch lease.Job().Type {
	case "scan_source":
		listing := d.listing
		if source := lease.Job().SourceID; source != nil && *source == "companySite" {
			listing = d.firecrawlListing
		}
		if listing == nil {
			return workerDispatchResult{}, ErrAIStageUnavailable
		}
		result, err := listing.Execute(ctx, lease)
		return workerDispatchResult{pending: !result.Done, code: result.PartialCode}, err
	case "fetch_full_text":
		detail := d.detail
		if source := lease.Job().SourceID; source != nil && *source == "companySite" {
			detail = d.firecrawlDetail
		}
		if detail == nil {
			return workerDispatchResult{}, ErrAIStageUnavailable
		}
		result, err := detail.Execute(ctx, lease)
		return workerDispatchResult{pending: result.Pending}, err
	case "import_vacancy":
		if d.importer == nil {
			return workerDispatchResult{}, ErrAIStageUnavailable
		}
		_, err := d.importer.Execute(ctx, lease)
		return workerDispatchResult{}, err
	case "classify_title", "extract_vacancy", "assign_roles", "score_relevance":
		if d.ai == nil {
			return workerDispatchResult{}, ErrAIStageUnavailable
		}
		_, err := d.ai.Execute(ctx, lease)
		return workerDispatchResult{finished: err == nil}, err
	default:
		return workerDispatchResult{}, ErrWorkerUnsupported
	}
}
