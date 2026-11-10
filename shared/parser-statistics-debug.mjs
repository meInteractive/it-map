// Diagnostic projections of already loaded Go API responses. Never copy a
// raw report: titles, error messages, company/vacancy names and job payloads
// can contain personal data or credentials.
export function parserIssuesSnapshot(report) {
  if (!report || !report.history || !Array.isArray(report.history.points) ||
      report.history.points.length !== 30 || !Array.isArray(report.issues)) {
    throw new Error('issues_history_unavailable');
  }
  const history=report.history;
  const measured=[...history.points].reverse().find(day=>day.observedAt!==null && day.observedAt!==undefined);
  const today=history.points.at(-1);
  // There is no persisted P2 historical status/type/source/reason breakdown:
  // parser_error_daily contains daily maxima, not a causal trace.
  const issues=report.issues.map(issue=>({
    issueId:issue.id,priority:issue.priority,status:issue.status,
    target:issue.target,type:issue.stage,sourceId:issue.sourceId,
    reasonCode:issue.reasonCode,observedAt:issue.observedAt??null,
    affected:issue.affected??null,
  }));
  return {
    dataAsOf:report.checkedAt,
    filters:{},
    summary:{
      checkedAt:report.checkedAt,
      metric:history.metric,
      activeCounts:report.activeCounts,
      historicalFailed:report.historicalFailed,
      sampleLimited:report.sampleLimited,
      limit:report.limit,
      days:history.days,
      observedDays:history.observedDays,
      average:history.average,
      lastMeasuredAt:measured?.observedAt??null,
      todayPeak:{
        p1:today?.observedAt===null || today?.observedAt===undefined ? null : today.p1,
        p2:today?.observedAt===null || today?.observedAt===undefined ? null : today.p2,
      },
      // Достоверная серверная разбивка по ВСЕМ активным проблемным заданиям,
      // а не реконструкция причин по двум датам или по 10 issues в интерфейсе.
      p2Breakdown:report.p2Breakdown??{available:false},
      historicalP2BreakdownAvailable:false,
    },
    items:issues,
    total:issues.length,
    coverage:'complete',
    // Keep ALL 30 calendar days. Missing measurements remain null, not zero.
    daily:history.points.map(p=>({
      day:p.day,p1:p.p1,p2:p.p2,observedAt:p.observedAt,
    })),
  };
}

const stageNames=['received','accepted','fulltext','structured','roles','published'];
export function parserFunnelSnapshot(report) {
  if (!report || !Array.isArray(report.daily) || report.daily.length !== 30 ||
      !report.filters || !report.cohort || !report.totals || !report.averages) {
    throw new Error('funnel_history_unavailable');
  }
  const count=report.cohort.received;
  const cohortRates={};
  for (const stage of stageNames) {
    const value=report.cohort[stage];
    cohortRates[stage]=Number.isFinite(count) && count>0 && Number.isFinite(value)
      ? value/count*100 : null;
  }
  return {
    dataAsOf:report.checkedAt,
    filters:{sourceId:report.filters.sourceId,parser:report.filters.parser},
    summary:{
      checkedAt:report.checkedAt,
      from:report.from,to:report.to,days:report.days,
      metric:'candidate_stage_events_and_unique_vacancy_creates',
      totals:report.totals,
      averages:report.averages,
      cohort:report.cohort,
      // The unique CREATE denominator uses first successful CREATE globally
      // for each real company_vacancies.id. Cohort.created counts candidates
      // with a CREATE, not distinct new catalog vacancies.
      uniqueNewVacanciesInPeriod:report.totals.newVacancies??null,
      undatedNewVacancies:report.undatedNewVacancies??null,
      // Только обезличенные числовые группы; названия, URL и ID не экспортируются.
      publicationWindow:report.publicationWindow&&Array.isArray(report.publicationWindow.days)
        ? {
          asOf:report.publicationWindow.asOf,
          weekFrom:report.publicationWindow.weekFrom,
          days:report.publicationWindow.days.map(day=>({
            day:day.day,total:day.total,inWeek:day.inWeek,
            older:day.older,future:day.future,undated:day.undated,
          })),
        } : null,
      cohortCandidatesWithCreate:report.cohort.created,
      cohortCandidatesLinkedToExisting:report.cohort.linked,
      conversionBase:'cohort_received',
      stageConversionPercent:cohortRates,
    },
    daily:report.daily.map(day=>({
      day:day.day,
      received:day.received,accepted:day.accepted,
      fulltext:day.fulltext,structured:day.structured,
      roles:day.roles,published:day.published,
      newVacancies:day.newVacancies??null,
    })),
  };
}

// Только разрешённые числовые агрегаты HH и настройки расписания.
// Копирование не перечитывает API, не раскрывает источники, jobs или Actor.
export function hhScanScheduleSnapshot(report) {
  if (!report || report.sourceId !== 'hh' || !report.summary)
    throw new Error('hh_schedule_unavailable');
  const s=report.summary;
  const p=report.pendingBreakdown;
  const coverage=s.successfulLast24h!==undefined &&
    s.successfulLast48h!==undefined && s.successDateUnknown!==undefined;
  return {
    dataAsOf:report.checkedAt,
    filters:{
      sourceId:'hh',favoritesOnly:report.favoritesOnly,
      intervalDays:report.intervalDays,vacancyAgeDays:report.vacancyAgeDays,
    },
    summary:{
      checkedAt:report.checkedAt,schedulerEnabled:report.enabled,
      sourceSelected:report.sourceSelected,
      configured:s.configured,enabledInScope:s.configured-s.disabled-s.outsideScope,
      disabled:s.disabled,outsideScope:s.outsideScope,
      potentiallyDue:s.potentiallyDue,waiting:s.waiting,activeScan:s.activeScan,
      nextDueAt:s.nextDueAt,recalculated:s.recalculated,
      unknownLastSuccess:s.unknownLastSuccess,
      coverageAvailable:coverage,
      successfulLast24h:coverage?s.successfulLast24h:null,
      successfulLast48h:coverage?s.successfulLast48h:null,
      successDateUnknown:coverage?s.successDateUnknown:null,
      pendingHHScans:report.pendingHHScans,runningHHScans:report.runningHHScans,
      pendingBreakdownAvailable:Boolean(p),
      pendingBreakdown:p?{
        pending:p.pending,notDue:p.notDue,unverifiedCursor:p.unverifiedCursor,
        orphanPaid:p.orphanPaid,reconcileCandidate:p.reconcileCandidate,
        launchCooldown:p.launchCooldown,launchCandidate:p.launchCandidate,
        other:p.other,oldestPendingAt:p.oldestPendingAt,
      }:null,
      finalEligibilityVerified:report.finalEligibilityVerified,
    },
  };
}
