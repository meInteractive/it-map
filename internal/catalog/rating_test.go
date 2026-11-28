package catalog

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCalculateCatalogRating(t *testing.T) {
	input := []byte(`{
		"companies":[{"name":"CI employer","score":0}],
		"agencies":[
		 {"name":"CI partial","rank":1,"score":7,"ethics":10},
		 {"name":"CI capped","rank":2,"ethics":2,"vacancyHonesty":10,"feedback":10,"vacancyRelevance":10,"itExpertise":10,"clientKnowledge":10,"processOrganization":10,"candidateSupport":10,"executiveSearch":"Да","agencyTypes":["IT Recruitment"],"revenue":{"amount":25000000}},
		 {"name":"CI full","rank":3,"ethics":10,"vacancyHonesty":10,"feedback":10,"vacancyRelevance":10,"itExpertise":10,"clientKnowledge":10,"processOrganization":10,"candidateSupport":10,"executiveSearch":"Частично","functionalSpecializations":["Product"],"roles":"technical product manager","revenue":{"amount":10000000000}}
		]}`)
	data, result, err := calculateCatalogRating(input, time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC))
	if err != nil || result != (RatingResult{true, 1, 2, 1}) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	var out sourceCatalog
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Companies[0]["score"] == float64(0) {
		t.Fatal("оценка компании не пересчитана")
	}
	for i, want := range []struct {
		name                               string
		score, reviews, relevance, revenue float64
	}{
		{"CI full", 96, 80, 6, 10}, {"CI capped", 60, 67.2, 10, 2}, {"CI partial", 7, 0, 0, 0},
	} {
		got := out.Agencies[i]
		if nameOf(got) != want.name || got["score"] != want.score || got["rank"] != float64(i+1) {
			t.Fatalf("позиция %d: %v", i, got)
		}
		if i < 2 && (got["reviewsScore"] != want.reviews || got["relevanceScore"] != want.relevance || got["revenueScore"] != want.revenue) {
			t.Fatalf("части рейтинга: %v", got)
		}
	}
}
