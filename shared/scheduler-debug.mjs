// Преобразования данных для JSON-диагностики: только текущий UI state,
// никаких дополнительных запросов, side effects или сырых текстов ошибок.
export function schedulerOverviewSnapshot(d) {
  return {
    summary:{
      enabled:d.settings.enabled,cloudStatus:d.cloudStatus,
      lastSignalAt:d.lastSignalAt,lastSuccessAt:d.lastSuccessAt,nextSignalAt:d.nextSignalAt,
      counts:d.counts,statistics:d.statistics,
      // dayUsd включает резервы — это НЕ подтверждённая стоимость.
      spending:{upperEstimateUsd:d.spending.dayUsd},
    },
  };
}
export function schedulerQueueSnapshot(d,filters) {
  return {
    filters:{status:filters.status,type:filters.type,searchApplied:Boolean(filters.searchApplied)},
    summary:{totalMatched:d.total},dataAsOf:d.checkedAt,
    items:d.items,total:d.total,coverage:'server_latest',
  };
}
export function schedulerRunsSnapshot(d) {
  return {
    summary:{totalMatched:null},
    items:d.runs.map(x=>({
      runId:x.id,trigger:x.trigger,status:x.status,
      startedAt:x.started_at,finishedAt:x.finished_at,
      ready:x.ready,processed:x.processed,succeeded:x.succeeded,
      errors:x.errors,remaining:x.remaining,reason:x.reason,
    })),
    // API /scheduler отдаёт последние 20 run. Полный count пока неизвестен.
    total:d.runs.length,coverage:'server_latest',
  };
}
export function schedulerAttemptsSnapshot(d) {
  return {
    summary:{totalMatched:null},
    items:d.attempts.map(x=>({
      attemptId:x.id,jobId:x.job_id,runId:x.run_id,type:x.type,sourceId:x.source_id,
      status:x.status,reasonCode:x.reason_code,
      provider:x.provider,model:x.model,
      startedAt:x.started_at,finishedAt:x.finished_at,
      reservedUsd:x.reserved_usd,costUsd:x.cost_usd,credits:x.credits,
    })),
    // API /scheduler отдаёт последние 50 attempts; число всех неизвестно.
    total:d.attempts.length,coverage:'server_latest',
  };
}
