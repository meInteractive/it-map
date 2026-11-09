package parser

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func repeatPrice(n float64) sql.NullFloat64 { return sql.NullFloat64{Valid: true, Float64: n} }

func TestGO434ApifyRepeatEvidenceDoesNotClaimSecondCharge(t *testing.T) {
	rows := []repeatActorEvidence{
		{Account: "ci", OperationKey: "scan", Generation: 1, RunID: "actor-a", ReservedUSD: .3, ChargedUSD: repeatPrice(.4)},
		{Account: "ci", OperationKey: "scan", Generation: 1, RunID: "actor-a", ReservedUSD: .3, ChargedUSD: repeatPrice(.4)},
		{Account: "ci", OperationKey: "scan", Generation: 1, RunID: "actor-b", ReservedUSD: .3, ChargedUSD: repeatPrice(.2)},
		{Account: "other", OperationKey: "scan", Generation: 1, RunID: "actor-a", ChargedUSD: repeatPrice(.1)},
		{Account: "ci", OperationKey: "scan", Generation: 2, RunID: "actor-c", ChargedUSD: repeatPrice(.1)},
		{Account: "ci", OperationKey: "retry", Generation: 1, RunID: "actor-conflict", ChargedUSD: repeatPrice(.5)},
		{Account: "ci", OperationKey: "retry", Generation: 1, RunID: "actor-conflict", ChargedUSD: repeatPrice(.6)},
		{Account: "ci", OperationKey: "lost-ack", Generation: 1, RunID: "", ReservedUSD: .9},
	}
	got, err := summarizeRepeatApify(rows)
	if err != nil { t.Fatal(err) }
	if got.LocalReceipts != 8 || got.ActorOperations != 5 || got.RepeatedReceiptGroups != 2 ||
		got.ExtraLocalReceipts != 2 || got.ConflictingActorGroups != 1 ||
		got.SuspectedLaunchGroups != 1 || got.ActorsInSuspectedGroups != 2 ||
		got.MissingActorIdentity != 1 || math.Abs(got.UnknownStartReserveUSD-.9)>1e-10 {
		t.Fatalf("actor receipt/repeat evidence misclassified: %+v",got)
	}
	for _, invalid := range []float64{-1,math.NaN(),math.Inf(1)} {
		_, err := summarizeRepeatApify([]repeatActorEvidence{{Account:"ci",RunID:"bad",ReservedUSD:invalid}})
		if !errors.Is(err,ErrExpenseRepeatsStorage) {t.Fatal("invalid reserve accepted", invalid)}
	}
}

func TestGO434AIRepeatedAttemptsAreNotProviderVerified(t *testing.T) {
	rows := []repeatAIEvidence{
		{JobID:"classify-1", Type:"classify_title", Provider:"openrouter", CostUSD:repeatPrice(.02)},
		{JobID:"classify-1", Type:"classify_title", Provider:"openrouter", ReservedUSD:.05},
		{JobID:"classify-1", Type:"classify_title", Provider:"openrouter", CostUSD:repeatPrice(0)},
		{JobID:"extract-1", Type:"extract_vacancy", Provider:"openai", CostUSD:repeatPrice(.11)},
		{JobID:"extract-1", Type:"extract_vacancy", Provider:"openai", CostUSD:repeatPrice(.12)},
		{JobID:"extract-1", Type:"extract_vacancy", Provider:"openrouter", CostUSD:repeatPrice(.21)},
		{JobID:"extract-1", Type:"assign_roles", Provider:"openai", ReservedUSD:.06},
		{JobID:"", Type:"extract_vacancy", Provider:"openai", CostUSD:repeatPrice(.42)},
	}
	got, err := summarizeRepeatAI(rows)
	if err != nil { t.Fatal(err) }
	if got.ObservedAttempts!=8 || got.RepeatGroups!=2 || got.ExtraAttempts!=3 || len(got.Stages)!=2 {
		t.Fatalf("repeated AI job groups not classified: %+v",got)
	}
	if got.Stages[0].Type!="classify_title" || got.Stages[0].RepeatGroups!=1 ||
		got.Stages[0].ExtraAttempts!=2 || got.Stages[0].KnownCostAttempts!=2 ||
		got.Stages[0].UnknownCostAttempts!=1 || got.Stages[0].KnownLocalCostUSD!=.02 {
		t.Fatalf("unexpected classify evidence: %+v",got.Stages[0])
	}
	if got.Stages[1].Type!="extract_vacancy" || got.Stages[1].ExtraAttempts!=1 ||
		math.Abs(got.Stages[1].KnownLocalCostUSD-.23)>1e-10 {
		t.Fatalf("unexpected extraction evidence: %+v",got.Stages[1])
	}
	if repeatStageType("PRIVATE_USER_INPUT")!="other" {t.Fatal("unexpected type exposed")}
	if _,err:=summarizeRepeatAI([]repeatAIEvidence{{JobID:"j",Provider:"openai",Type:"classify_title",CostUSD:repeatPrice(math.NaN())}});!errors.Is(err,ErrExpenseRepeatsStorage){
		t.Fatal("NaN accepted",err)
	}
	payload,_:=json.Marshal(expenseRepeatsReport{AI:got,ProviderRepeatVerified:false,WindowLimited:true})
	for _, private:=range []string{"classify-1","extract-1","PRIVATE_USER_INPUT"} {
		if strings.Contains(string(payload),private) {t.Fatal("raw job identifier leaked",private)}
	}
}
