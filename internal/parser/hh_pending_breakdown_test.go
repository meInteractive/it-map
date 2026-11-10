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
	if got.UnverifiedLineage != 1 || got.MissingOperation != 1 ||
		got.OldestUnverifiedAt == nil || *got.OldestUnverifiedAt != now-7200000 ||
		got.OldestReconcileAt == nil || *got.OldestReconcileAt != now-7200000 ||
		got.OldestCooldownAt != nil {
		t.Fatalf("не разделены два вида неподтверждённого курсора: %+v", got)
	}
	// Новый Actor ограничен часовым интервалом, но дочитывание
	// уже оплаченного Dataset остаётся допустимым.
	view.lastListingStart = now - int64(time.Minute/time.Millisecond)
	got = summarizeHHPending(jobs, now, settings, view)
	if got.ReconcileCandidate != 1 || got.LaunchCandidate != 0 || got.LaunchCooldown != 1 ||
		got.OldestCooldownAt == nil || *got.OldestCooldownAt != now-7200000 {
		t.Fatalf("ограничение launch помешало безопасному reconcile: %+v", got)
	}
	view.lastListingStart = now - int64(hhActorLaunchCooldown/time.Millisecond)
	got = summarizeHHPending(jobs, now, settings, view)
	if got.ReconcileCandidate != 1 || got.LaunchCandidate != 1 || got.LaunchCooldown != 0 {
		t.Fatalf("ровно через 15 минут cooldown должен закончиться: %+v", got)
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

func TestHHPendingBreakdownUnconfirmedPaidRunIsNotReconcile(t *testing.T) {
	now := time.Date(2026, 11, 10, 16, 30, 0, 0, time.UTC).UnixMilli()
	source := "hh"
	job := Job{ID: "uncertain", Type: "scan_source", Status: "pending", SourceID: &source, Payload: json.RawMessage("{}"), RunAfter: now - 1000, CreatedAt: now - int64(6*24*time.Hour/time.Millisecond)}
	key := "go398:CI_UNCONFIRMED"
	view := claimView{
		busy: map[int64]bool{}, pages: map[string]string{},
		operations: map[string]bool{key: true}, goOperationKeys: map[string]string{job.ID: key},
		unconfirmedRunIDs: map[string]bool{key: true},
		verifiedHH: map[hhCandidateKey]bool{}, blockedHHScans: map[string]bool{},
		lastListingStart: now - int64(time.Hour/time.Millisecond),
	}
	settings := DefaultSettings()
	settings.Enabled = true
	got := summarizeHHPending([]Job{job}, now, settings, view)
	if got.Pending != 1 || got.Other != 1 || got.UnconfirmedRunID != 1 ||
		got.LaunchCandidate != 0 || got.ReconcileCandidate != 0 || got.LaunchCooldown != 0 ||
		got.OldestUnconfirmedAt == nil || *got.OldestUnconfirmedAt != job.CreatedAt {
		t.Fatalf("неподтверждённый Run ID должен оставаться в безопасной other-категории: %+v", got)
	}
	delete(view.unconfirmedRunIDs, key)
	got = summarizeHHPending([]Job{job}, now, settings, view)
	if got.Pending != 1 || got.Other != 0 || got.UnconfirmedRunID != 0 ||
		got.ReconcileCandidate != 1 || got.OldestUnconfirmedAt != nil {
		t.Fatalf("после подтверждения Run ID должен разрешаться только GET/reconcile: %+v", got)
	}
}

func TestHHPendingBreakdownUnverifiedLineageReasonsPartition(t *testing.T) {
	now := time.Date(2026,11,10,17,0,0,0,time.UTC).UnixMilli()
	source := "hh"
	jobs := []Job{}
	blocked := map[string]bool{}
	reasons := map[string]string{}
	for i, reason := range []string{hhBlockedSourceChanged, hhBlockedLegacyFilters, hhBlockedMalformedCursor, hhBlockedOtherProof} {
		id := string(rune('a'+i))
		jobs = append(jobs, Job{ID:id, Type:"scan_source", Status:"pending", SourceID:&source, Payload:json.RawMessage("{}"), CreatedAt:now-1000, RunAfter:now})
		blocked[id]=true
		reasons[id]=reason
	}
	view := claimView{blockedHHScans:blocked}
	got := summarizeHHPendingWithReasons(jobs,now,DefaultSettings(),view,reasons)
	if got.Pending!=4 || got.UnverifiedCursor!=4 || got.UnverifiedLineage!=4 ||
		got.UnverifiedSourceChanged!=1 || got.UnverifiedLegacyFilters!=1 ||
		got.UnverifiedMalformedCursor!=1 || got.UnverifiedOtherProof!=1 ||
		got.MissingOperation!=0 || got.Other!=0 || got.ReconcileCandidate!=0 || got.LaunchCandidate!=0 {
		t.Fatalf("причины HH блокировки должны делить только unverifiedLineage: %+v",got)
	}
	// Без доступной классификации старые тестовые входы остаются безопасно заблокированы.
	got = summarizeHHPending(jobs,now,DefaultSettings(),view)
	if got.UnverifiedOtherProof!=4 || got.UnverifiedLineage!=4 {t.Fatalf("неизвестная причина блокировки: %+v",got)}
}

func TestHHPendingLegacyFactorsCanOverlap(t *testing.T) {
    now:=time.Date(2026,11,10,19,0,0,0,time.UTC).UnixMilli()
    source:="hh"
    jobs:=[]Job{{ID:"one",Type:"scan_source",Status:"pending",SourceID:&source,RunAfter:now},
        {ID:"two",Type:"scan_source",Status:"pending",SourceID:&source,RunAfter:now}}
    view:=claimView{blockedHHScans:map[string]bool{"one":true,"two":true}}
    reasons:=map[string]string{"one":hhBlockedLegacyFilters,"two":hhBlockedLegacyFilters}
    legacy:=map[string]hhLegacyFactors{"one":{true,true,true,true,true,true},"two":{Roles:true}}
    b:=summarizeHHPendingWithDetails(jobs,now,DefaultSettings(),view,reasons,legacy)
    if b.Pending!=2||b.UnverifiedLineage!=2||b.UnverifiedLegacyFilters!=2||
        b.UnverifiedLegacyVersion!=1||b.UnverifiedLegacyLineage!=1||
        b.UnverifiedLegacyMoscow!=1||b.UnverifiedLegacyDate!=1||
        b.UnverifiedLegacyTitle!=1||b.UnverifiedLegacyRoles!=2||
        b.ReconcileCandidate!=0||b.LaunchCandidate!=0 {
        t.Fatalf("вложенные признаки не должны изменять статус задания: %+v",b)
    }
}
