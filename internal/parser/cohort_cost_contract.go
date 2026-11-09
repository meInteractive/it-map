package parser

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// GO-433 future provider receipt/cohort contract. This pure reconciler is
// intentionally NOT wired to the live parser, database or billing. It tests
// which durable identities and evidence must exist before displaying cohort
// unit costs. A stored local cost is NOT an externally verified charge.
type cohortOperation struct {
	Kind              string   // apify_actor, apify_dataset_read, ai_provider_request, firecrawl_request, ai_attempt
	LocalReceiptID    string   // internal ledger-local ID; never a public job/vacancy ID
	BillingScope      string   // provider account scope, stored privately
	ProviderOperation string   // actual externally confirmed request/run ID, NOT dataset ID or scheduler cycle
	ObservedUSD       *float64 // locally observed amount, including explicit 0
	ReservedUSD       *float64 // upper budget exposure; never an actual charge
	ProviderVerified  bool     // only true after an independent provider confirmation
}

type cohortEdge struct {
	Kind           string
	LocalReceiptID string
	CohortID       string
	Captured       bool // true ONLY for an event written by trusted server code at execution time
}

type cohortCostItem struct {
	ID          string
	VerifiedUSD float64
}

type cohortCostReport struct {
	Receipts                int
	Operations              int
	DuplicateReceipts       int
	DuplicateLinks          int
	UnsupportedLinks        int
	ConflictingOperations   int
	UnpricedOperations      int
	UnknownReserveOperations int
	UnknownReserveUSD       float64
	LocalObservedUSD        float64
	ProviderVerifiedUSD     float64
	LocalUnverifiedUSD      float64
	LinkedVerifiedUSD       float64
	UnlinkedVerifiedUSD     float64
	SharedVerifiedUSD       float64
	ByCohort               []cohortCostItem
}

var errCohortContract = errors.New("incomplete or conflicting cost lineage contract")

type cohortOperationGroup struct {
	cost       *float64
	reserve    float64
	hasReserve bool
	verified   bool
	providerID string
	conflict   bool
	cohorts    map[string]bool
}
type cohortLocalIdentity struct {
	operationKey string
	billingScope string
	providerID   string
}

func cohortKey(kind,scope,providerID,localID string) string {
	if providerID!="" && scope!="" && kind!="ai_attempt" {
		// Length-prefix prevents accidental merging if identifiers contain delimiters.
		return "external:" + kind+":"+strconv.Itoa(len(scope))+":"+scope+":"+providerID
	}
	return "local:" + kind+":"+localID
}
func cohortIdentifier(v string) bool {
	if v=="" || len(v)>256 || strings.TrimSpace(v)!=v {return false}
	for _,r:=range v {
		if r<33 || r==127 {return false}
	}
	return true
}
func cohortNumber(v *float64) bool {
	return v==nil||(*v>=0&&!math.IsNaN(*v)&&!math.IsInf(*v,0))
}
func cohortKind(kind string) bool {
	switch kind {
	case "apify_actor","apify_dataset_read","ai_provider_request","firecrawl_request","ai_attempt":
		return true
	default:return false
	}
}

// reconcileCohortEvidence operates on synthetic or already-validated internal
// evidence. It never accesses a provider or uses URL/time/source inference.
//
// Invariants:
// - distinct external operations are identified by (kind, account, provider ID)
// - independent accounts never collapse; duplicate local observations do
// - a paid empty scan is included even when it has zero candidate edges
// - an external operation linked to several cohorts is NOT fully allocated
//   to every cohort, nor arbitrarily assigned to the earliest cohort
// - local AI attempt totals remain separate from provider-verified money
// - missing provider identity, settlement or trusted capture => unallocated
func reconcileCohortEvidence(receipts []cohortOperation,edges []cohortEdge) (cohortCostReport,error) {
	out:=cohortCostReport{Receipts:len(receipts)}
	groups:=make(map[string]*cohortOperationGroup)
	local:=make(map[string]cohortLocalIdentity)
	for _,x:=range receipts {
		if !cohortKind(x.Kind)||!cohortIdentifier(x.LocalReceiptID)||
			!cohortNumber(x.ObservedUSD)||!cohortNumber(x.ReservedUSD)||
			(x.ProviderOperation!=""&&!cohortIdentifier(x.ProviderOperation))||
			(x.BillingScope!=""&&!cohortIdentifier(x.BillingScope))||
			(x.ProviderOperation!=""&&x.BillingScope=="")||
			(x.Kind=="ai_attempt"&&x.ProviderOperation!="")||
			(x.ProviderVerified&&(x.ObservedUSD==nil||x.ProviderOperation=="")) {
			return cohortCostReport{},errCohortContract
		}
		if x.Kind=="firecrawl_request" && x.ObservedUSD!=nil && !x.ProviderVerified {
			// Credit observations must never be silently interpreted as USD.
			return cohortCostReport{},errCohortContract
		}
		id:=x.Kind+":"+x.LocalReceiptID
		key:=cohortKey(x.Kind,x.BillingScope,x.ProviderOperation,x.LocalReceiptID)
		if earlier,ok:=local[id];ok {
			if earlier.operationKey!=key||earlier.billingScope!=x.BillingScope||earlier.providerID!=x.ProviderOperation {
				return cohortCostReport{},errCohortContract
			}
		} else {
			local[id]=cohortLocalIdentity{operationKey:key,billingScope:x.BillingScope,providerID:x.ProviderOperation}
		}
		g:=groups[key]
		if g==nil {g=&cohortOperationGroup{cohorts:make(map[string]bool)};groups[key]=g
		}else{out.DuplicateReceipts++}
		if x.ObservedUSD!=nil {
			if g.cost!=nil && *g.cost!=*x.ObservedUSD {g.conflict=true}
			v:=*x.ObservedUSD
			g.cost=&v
		}
		if x.ReservedUSD!=nil {
			if !g.hasReserve||*x.ReservedUSD>g.reserve {g.reserve=*x.ReservedUSD}
			g.hasReserve=true
		}
		if x.ProviderVerified {g.verified=true}
		if x.ProviderOperation!="" {g.providerID=x.ProviderOperation}
	}
	for _,e:=range edges {
		if !cohortKind(e.Kind)||!cohortIdentifier(e.LocalReceiptID)||
			!cohortIdentifier(e.CohortID) {
			return cohortCostReport{},errCohortContract
		}
		identity,ok:=local[e.Kind+":"+e.LocalReceiptID]
		if !ok{return cohortCostReport{},errCohortContract}
		if !e.Captured {
			out.UnsupportedLinks++
			continue
		}
		g:=groups[identity.operationKey]
		if g.cohorts[e.CohortID] {out.DuplicateLinks++
		}else{g.cohorts[e.CohortID]=true}
	}
	keys:=make([]string,0,len(groups))
	for k:=range groups{keys=append(keys,k)}
	sort.Strings(keys)
	totals:=map[string]float64{}
	for _,key:=range keys {
		g:=groups[key]
		out.Operations++
		if g.conflict {
			// Even with a provider ID, incompatible monetary observations
			// are not a trustworthy invoice. No arbitrary MAX.
			out.ConflictingOperations++
			continue
		}
		if g.cost==nil {
			if g.hasReserve&&g.reserve>0{
				out.UnknownReserveOperations++
				out.UnknownReserveUSD+=g.reserve
			}else{out.UnpricedOperations++}
			continue
		}
		value:=*g.cost
		out.LocalObservedUSD+=value
		if !g.verified||g.providerID=="" {
			out.LocalUnverifiedUSD+=value
			continue
		}
		out.ProviderVerifiedUSD+=value
		switch len(g.cohorts) {
		case 0:
			out.UnlinkedVerifiedUSD+=value
		case 1:
			for id:=range g.cohorts{totals[id]+=value}
			out.LinkedVerifiedUSD+=value
		default:
			out.SharedVerifiedUSD+=value
		}
	}
	names:=make([]string,0,len(totals))
	for id:=range totals{names=append(names,id)}
	sort.Strings(names)
	for _,id:=range names {
		out.ByCohort=append(out.ByCohort,cohortCostItem{ID:id,VerifiedUSD:totals[id]})
	}
	return out,nil
}
