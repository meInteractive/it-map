package parser

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHHPendingBreakdownSeparatesReadinessWithoutStartingActor(t *testing.T) {
	now := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC).UnixMilli()
	source := "hh"
	makeJob := func(id string, due int64) Job {
		return Job{
			ID: id, Type: "scan_source", Status: "pending", SourceID: &source,
			Payload: json.RawMessage("{}"), RunAfter: due, CreatedAt: now - 7200000,
		}
	}
	settings := DefaultSettings()
	settings.Enabled = true
	view := claimView{
		busy: map[int64]bool{}, pages: map[string]string{},
		operations: map[string]bool{"go398:paid": true},
		goOperationKeys: map[string]string{"reconcile": "go398:paid"},
		orphanPaidScans: map[string]bool{"orphan": true},
		blockedHHScans: map[string]bool{"blocked": true},
		verifiedHH: map[hhCandidateKey]bool{},
	}
	view.pages["go399:unverified"] = "0"
	jobs := []Job{
		makeJob("future", now+1000),
		makeJob("blocked", now),
		makeJob("orphan", now),
		makeJob("unverified", now),
		makeJob("reconcile", now),
		makeJob("launch", now),
		{ID: "other-source", Type: "scan_source", Status: "pending"},
	}
	got := summarizeHHPending(jobs, now, settings, view)
	if got.Pending != 6 || got.NotDue != 1 || got.UnverifiedCursor != 2 ||
		got.OrphanPaid != 1 || got.ReconcileCandidate != 1 || got.LaunchCandidate != 1 ||
		got.LaunchCooldown != 0 || got.Other != 0 || got.OldestPendingAt == nil ||
		*got.OldestPendingAt != now-7200000 {
		t.Fatalf("неверная разбивка HH-очереди: %+v", got)
	}
	// Новый Actor ограничен часовым интервалом, но дочитывание
	// уже оплаченного Dataset остаётся допустимым.
	view.lastListingStart = now - int64(time.Minute/time.Millisecond)
	got = summarizeHHPending(jobs, now, settings, view)
	if got.ReconcileCandidate != 1 || got.LaunchCandidate != 0 || got.LaunchCooldown != 1 {
		t.Fatalf("ограничение launch помешало безопасному reconcile: %+v", got)
	}
}

func TestHHPendingBreakdownMalformedPayloadIsNotCountedAsReady(t *testing.T) {
	now := int64(1790000000000)
	source := "hh"
	job := Job{ID: "malformed", Type: "scan_source", Status: "pending",
		SourceID: &source, Payload: json.RawMessage("{"), RunAfter: now}
	got := summarizeHHPending([]Job{job}, now, DefaultSettings(), claimView{})
	if got.Pending != 1 || got.Other != 1 || got.LaunchCandidate != 0 ||
		got.ReconcileCandidate != 0 {
		t.Fatalf("повреждённый payload нельзя считать готовым: %+v", got)
	}
}
