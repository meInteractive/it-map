package parser

import (
	"errors"
	"math"
	"testing"
)

func cohortUSD(v float64) *float64{return &v}
func cohortReceipt(kind,local,account,external string,observed,reserve *float64,verified bool) cohortOperation {
	return cohortOperation{Kind:kind,LocalReceiptID:local,BillingScope:account,
		ProviderOperation:external,ObservedUSD:observed,ReservedUSD:reserve,ProviderVerified:verified}
}
func cohortCaptured(kind,local,cohort string) cohortEdge{
	return cohortEdge{Kind:kind,LocalReceiptID:local,CohortID:cohort,Captured:true}
}
func nearMoney(a,b float64)bool{return math.Abs(a-b)<1e-9}

func TestCohortReceiptOnePaidOperationManyCandidatesAndScans(t *testing.T){
	const a="scan-one",b="scan-two"
	rows:=[]cohortOperation{
		// A single Actor can yield several candidates and can be reread
		// from Dataset. It remains exactly one invoice.
		cohortReceipt("apify_actor","actor-a","account-1","provider-actor-1",cohortUSD(.6),cohortUSD(.8),true),
		cohortReceipt("apify_actor","actor-b","account-1","provider-actor-1",cohortUSD(.6),cohortUSD(.8),true),
		cohortReceipt("apify_actor","actor-a","account-1","provider-actor-1",cohortUSD(.6),cohortUSD(.8),true),
		// A paid empty Actor scan still belongs in the cohort numerator.
		cohortReceipt("apify_actor","actor-empty","account-1","empty-provider-run",cohortUSD(.2),nil,true),
		// Reused across different scan cohorts: don't arbitrarily assign
		// the whole amount twice, nor divide it into guessed fractions.
		cohortReceipt("apify_actor","actor-shared","account-1","provider-actor-shared",cohortUSD(.4),nil,true),
		// Confirmed receipt with no trusted link must remain unallocated.
		cohortReceipt("apify_actor","actor-unlinked","account-1","provider-actor-unlinked",cohortUSD(.15),nil,true),
		cohortReceipt("ai_provider_request","ai-provider","model-account","ci-ai-request",cohortUSD(.05),nil,true),
		// Scheduler attempt ID is NOT a provider request ID. It may
		// aggregate several requests and is only locally observed money.
		cohortReceipt("ai_attempt","scheduler-attempt","","",cohortUSD(.03),nil,false),
		cohortReceipt("firecrawl_request","fc-provider","fc-account","ci-fc-bill-id",cohortUSD(.12),nil,true),
		// Dataset GET is a separate kind and must be priced separately
		// from Actor; don't invent a link from the dataset ID.
		cohortReceipt("apify_dataset_read","dataset-read","account-1","provider-dataset-invoice",cohortUSD(.08),nil,true),
		// Lost ACK or reservation isn't an invoice.
		cohortReceipt("apify_actor","actor-unknown","account-1","",nil,cohortUSD(.25),false),
		// 0 reserve + NULL cost does not prove free.
		cohortReceipt("ai_attempt","unpriced-attempt","","",nil,cohortUSD(0),false),
		// Conflicting prices for one provider operation cannot be silently
		// reconciled to MAX() or SUM().
		cohortReceipt("apify_actor","conflict-one","account-1","ci-conflict",cohortUSD(.2),nil,true),
		cohortReceipt("apify_actor","conflict-two","account-1","ci-conflict",cohortUSD(.25),nil,true),
	}
	links:=[]cohortEdge{
		cohortCaptured("apify_actor","actor-a",a),
		cohortCaptured("apify_actor","actor-b",a),
		cohortCaptured("apify_actor","actor-b",a),
		cohortCaptured("apify_actor","actor-empty",a),
		cohortCaptured("apify_actor","actor-shared",a),
		cohortCaptured("apify_actor","actor-shared",b),
		cohortCaptured("ai_provider_request","ai-provider",b),
		cohortCaptured("ai_attempt","scheduler-attempt",a),
		cohortCaptured("firecrawl_request","fc-provider",a),
		cohortCaptured("apify_dataset_read","dataset-read",a), // weak source tracing, not signed at read
		cohortCaptured("apify_actor","actor-unknown",a),
		cohortCaptured("apify_actor","conflict-one",a),
	}
	links[9].Captured=false
	got,err:=reconcileCohortEvidence(rows,links)
	if err!=nil{t.Fatal(err)}
	if got.Receipts!=len(rows)||got.Operations!=11||got.DuplicateReceipts!=4||
		got.DuplicateLinks!=2||got.UnsupportedLinks!=1||
		got.ConflictingOperations!=1||got.UnpricedOperations!=1||
		got.UnknownReserveOperations!=1||!nearMoney(got.UnknownReserveUSD,.25){
		t.Fatalf("counting duplicate invoices/reserves failed: %+v",got)
	}
	if !nearMoney(got.ProviderVerifiedUSD,1.6)||
		!nearMoney(got.LocalObservedUSD,1.63)||!nearMoney(got.LocalUnverifiedUSD,.03)||
		!nearMoney(got.LinkedVerifiedUSD,.97)||!nearMoney(got.SharedVerifiedUSD,.4)||
		!nearMoney(got.UnlinkedVerifiedUSD,.23) {
		t.Fatalf("shared/unverified/unlinked dollars were allocated incorrectly: %+v",got)
	}
	if len(got.ByCohort)!=2||got.ByCohort[0].ID!=a||
		!nearMoney(got.ByCohort[0].VerifiedUSD,.92)||
		got.ByCohort[1].ID!=b||!nearMoney(got.ByCohort[1].VerifiedUSD,.05){
		t.Fatalf("incorrect single-owned provider-cost cohort split: %+v",got.ByCohort)
	}
}

func TestCohortProviderIdentityAndConfirmedZero(t *testing.T){
	rows:=[]cohortOperation{
		cohortReceipt("apify_actor","local-a","account-A","provider-run-x",cohortUSD(.4),nil,true),
		cohortReceipt("apify_actor","local-b","account-B","provider-run-x",cohortUSD(.4),nil,true),
		cohortReceipt("apify_actor","local-c","account-A","provider-run-y",cohortUSD(0),nil,true),
		cohortReceipt("ai_attempt","local-zero","","",cohortUSD(0),nil,false),
		cohortReceipt("firecrawl_request","credit-unknown","","",nil,nil,false),
	}
	edges:=[]cohortEdge{
		cohortCaptured("apify_actor","local-a","cohort-A"),
		cohortCaptured("apify_actor","local-b","cohort-B"),
		cohortCaptured("apify_actor","local-c","cohort-A"),
		cohortCaptured("ai_attempt","local-zero","cohort-B"),
	}
	got,err:=reconcileCohortEvidence(rows,edges)
	if err!=nil{t.Fatal(err)}
	if got.Operations!=5||got.UnpricedOperations!=1||
		!nearMoney(got.ProviderVerifiedUSD,.8)||
		!nearMoney(got.LinkedVerifiedUSD,.8)||
		len(got.ByCohort)!=2||!nearMoney(got.ByCohort[0].VerifiedUSD,.4)||
		!nearMoney(got.ByCohort[1].VerifiedUSD,.4){
		t.Fatalf("accounts collapsed or explicit zero misclassified: %+v",got)
	}
}

func TestCohortReceiptRejectsUntrustedOrIncompatibleEvidence(t *testing.T){
	bad:=[]struct{name string;rows []cohortOperation;edges []cohortEdge}{
		{"unknown kind",[]cohortOperation{{Kind:"payroll",LocalReceiptID:"ok"}},nil},
		{"no receipt identity",[]cohortOperation{{Kind:"apify_actor",LocalReceiptID:""}},nil},
		{"billing account missing",[]cohortOperation{
			cohortReceipt("apify_actor","id","","external",cohortUSD(.1),nil,true)},nil},
		{"fake paid ack",[]cohortOperation{
			cohortReceipt("apify_actor","id","account","",cohortUSD(.1),nil,true)},nil},
		{"no settled charge",[]cohortOperation{
			cohortReceipt("ai_provider_request","id","account","external",nil,cohortUSD(.1),true)},nil},
		{"scheduler attempt isn't provider request",[]cohortOperation{
			cohortReceipt("ai_attempt","id","account","fake-external",cohortUSD(.1),nil,true)},nil},
		{"negative cost",[]cohortOperation{
			cohortReceipt("ai_attempt","id","","",cohortUSD(-.1),nil,false)},nil},
		{"infinite reserve",[]cohortOperation{
			cohortReceipt("ai_attempt","id","","",nil,cohortUSD(math.Inf(1)),false)},nil},
		{"nan cost",[]cohortOperation{
			cohortReceipt("ai_attempt","id","","",cohortUSD(math.NaN()),nil,false)},nil},
		{"credits aren't USD",[]cohortOperation{
			cohortReceipt("firecrawl_request","id","","",cohortUSD(2),nil,false)},nil},
		{"rebinding same receipt",[]cohortOperation{
			cohortReceipt("apify_actor","id","account","external-1",cohortUSD(.1),nil,true),
			cohortReceipt("apify_actor","id","account","external-2",cohortUSD(.1),nil,true)},nil},
		{"dangling edge",[]cohortOperation{
			cohortReceipt("ai_attempt","id","","",nil,nil,false)},
			[]cohortEdge{cohortCaptured("ai_attempt","missing","cohort")}},
		{"bad cohort",[]cohortOperation{
			cohortReceipt("ai_attempt","id","","",nil,nil,false)},
			[]cohortEdge{cohortCaptured("ai_attempt","id"," \nnot-private")}},
	}
	for _,tt:=range bad {
		t.Run(tt.name,func(t *testing.T){
			if _,err:=reconcileCohortEvidence(tt.rows,tt.edges);
				!errors.Is(err,errCohortContract) {
				t.Fatalf("reconciliation accepted incompatible evidence: %v",err)
			}
		})
	}
}

func TestCohortUnprovenRelationshipDoesNotAllocateMoney(t *testing.T){
	rows:=[]cohortOperation{
		cohortReceipt("apify_actor","a","account","actor",cohortUSD(.75),nil,true),
		cohortReceipt("ai_attempt","ai","","",cohortUSD(.12),nil,false),
	}
	// An inferred main-vacancy relationship is not a captured source event.
	edges:=[]cohortEdge{
		{Kind:"apify_actor",LocalReceiptID:"a",CohortID:"guess-by-URL",Captured:false},
		cohortCaptured("ai_attempt","ai","guess-by-main-vacancy"),
	}
	got,err:=reconcileCohortEvidence(rows,edges)
	if err!=nil{t.Fatal(err)}
	if got.UnsupportedLinks!=1||len(got.ByCohort)!=0||
		!nearMoney(got.UnlinkedVerifiedUSD,.75)||!nearMoney(got.LocalUnverifiedUSD,.12){
		t.Fatalf("guessed cohort linkage unexpectedly became a charge: %+v",got)
	}
}
