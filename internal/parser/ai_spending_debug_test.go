package parser

import (
	"database/sql"
	"math"
	"testing"
)

func TestAISpendingCostSeparatesConfirmedUnknownAndFree(t *testing.T) {
	var s aiCostSummary
	if !s.add(0.02, sql.NullFloat64{Valid:true,Float64:0.013}) {
		t.Fatal("confirmed")
	}
	if !s.add(0.1, sql.NullFloat64{Valid:false}) {
		t.Fatal("unknown reserve")
	}
	if !s.add(0.2, sql.NullFloat64{Valid:true,Float64:0}) {
		t.Fatal("known free")
	}
	if !s.add(0, sql.NullFloat64{Valid:false}) {
		t.Fatal("unpriced attempt")
	}
	if math.Abs(s.ConfirmedUsd-0.013)>1e-12 ||
		math.Abs(s.UnknownReservedUsd-0.1)>1e-12 ||
		math.Abs(s.UpperEstimateUsd-0.113)>1e-12 ||
		s.UnknownCostOperations!=1 || s.UnpricedOperations!=1 {
		t.Fatalf("wrong bill separation: %+v",s)
	}
	before := s
	for _,value := range []float64{-1,math.NaN(),math.Inf(1)} {
		if s.add(value,sql.NullFloat64{}) {t.Fatal("invalid reserve accepted")}
	}
	if s.add(0,sql.NullFloat64{Valid:true,Float64:math.NaN()}) {
		t.Fatal("invalid cost accepted")
	}
	if s!=before {t.Fatal("failed validation mutated cost ledger")}
}
