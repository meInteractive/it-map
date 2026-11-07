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
      p2Breakdown:{available:false},
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
      metric:'stage_event_daily_counts',
      totals:report.totals,
      averages:report.averages,
      cohort:report.cohort,
      // These values concern the selected incoming cohort, not all
      // historically published vacancies or a count of scheduler attempts.
      newVacanciesPublished:report.cohort.created,
      linkedToExisting:report.cohort.linked,
      conversionBase:'cohort_received',
      stageConversionPercent:cohortRates,
    },
    daily:report.daily.map(day=>({
      day:day.day,
      received:day.received,accepted:day.accepted,
      fulltext:day.fulltext,structured:day.structured,
      roles:day.roles,published:day.published,
    })),
  };
}
