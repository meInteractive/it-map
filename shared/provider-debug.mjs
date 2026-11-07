// Безопасные снимки уже сохранённых бюджетов. Никаких запросов к провайдерам,
// ключей, URL Actor, accountId, ошибок подключения или исходных JSON.
function stamp(value) {
  if (typeof value !== 'string' && typeof value !== 'number') return null;
  const ms = typeof value === 'number' ? value : Date.parse(value);
  return Number.isFinite(ms) && ms >= 0 && ms < 253402300800000 ? new Date(ms).toISOString() : null;
}
function apifyProfile(budget) {
  if (!budget) return null;
  return {
    providerUsedUsd:budget.providerUsedUsd ?? null,
    ownConfirmedUsd:budget.ownConfirmedUsd ?? null,
    readReserveUsd:budget.readReserveUsd ?? null,
    unknownReserveUsd:budget.unknownReserveUsd ?? null,
    unknownRuns:budget.unknownRuns ?? null,
    limitUsd:budget.limitUsd ?? null,
    remainingUsd:budget.remainingUsd ?? null,
    periodStart:stamp(budget.periodStart),
    periodEnd:stamp(budget.periodEnd),
    checkedAt:stamp(budget.checkedAt),
  };
}
export function apifyUsageSnapshot(usage,operations=null) {
  // Реальный журнал Actor берётся отдельным read-only GET; список active
  // из /usage оставлен только как явно неполный fallback.
  const hasHistory=operations!==null && Array.isArray(operations.items) &&
    Number.isSafeInteger(operations.total) && operations.total>=0 && operations.coverage==='server_latest';
  const items = hasHistory ? operations.items.map(run=>({
    operationId:run.operationId,profile:run.profile,state:run.state,
    createdAt:run.createdAt,updatedAt:run.updatedAt,
    chargedUsd:run.chargedUsd,reservedUsd:run.reservedUsd,errorCode:run.errorCode,
  })) : (Array.isArray(usage.active) ? usage.active : []).map(run=>({
    operationId:run.id,
    profile:run.profile,
    state:run.state,
    createdAt:run.created_at,
    updatedAt:null,
    chargedUsd:null,
    reservedUsd:null,
  }));
  return {
    filters:{profile:'all'},
    summary:{
      providerLimitMode:usage.providerLimitMode ?? null,
      activeOnly:!hasHistory,
      operationHistoryAvailable:hasHistory,
      profiles:{
        free:apifyProfile(usage.profiles?.free),
        starter:apifyProfile(usage.profiles?.starter),
      },
    },
    // Список из SQL — именно последние 10 из всех совпадений.
    // Старый fallback /usage отражает только активные операции.
    dataAsOf:hasHistory?operations.checkedAt:null,
    items,total:hasHistory?operations.total:items.length,
    coverage:hasHistory?'server_latest':'complete',
  };
}
export function firecrawlUsageSnapshot(view,now=Date.now()) {
  const checked=view.checkedAt;
  const fresh=Boolean(view.fresh && typeof checked === 'number' && now >= checked && now-checked<=60000);
  const budget=view.budget;
  return {
    filters:{mode:view.mode},
    dataAsOf:checked,
    summary:{
      mode:view.mode,
      limitsCredits:view.limits?.credits ?? null,
      providerPlanCredits:budget?.providerPlanCredits ?? null,
      providerRemainingCredits:budget?.providerRemaining ?? null,
      providerUsedEstimateCredits:budget?.providerUsedEstimate ?? null,
      reportedCredits:budget?.localReported ?? null,
      estimatedCredits:budget?.localEstimated ?? null,
      reservedCredits:budget?.reserved ?? null,
      effectiveUsedCredits:budget?.effectiveUsed ?? null,
      remainingCredits:budget?.remaining ?? null,
      periodStart:stamp(budget?.periodStart),
      periodEnd:stamp(budget?.periodEnd),
      checkedAt:checked,
      fresh,
      budgetAvailable:Boolean(budget),
      operationHistoryAvailable:false,
      status:view.status,
    },
  };
}
