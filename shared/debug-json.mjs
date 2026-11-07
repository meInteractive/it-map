// Чистый сериализатор диагностики: никогда не копирует исходный HTTP JSON целиком.
export const DEBUG_JSON_SCHEMA_VERSION = 1;
export const DEBUG_JSON_EVENT_LIMIT = 10;
export const DEBUG_JSON_MAX_BYTES = 200 * 1024;
const stage = 'code', number = 'number', time = 'time', bool = 'boolean', day = 'day';
const counts = {pending:number,running:number,completed:number,failed:number,cancelled:number};
const stages = {scan_source:number,classify_title:number,fetch_full_text:number,extract_vacancy:number,assign_roles:number,import_vacancy:number,score_relevance:number};
const spent = {confirmedUsd:number,unknownReservedUsd:number,upperEstimateUsd:number,credits:number,unknownCostOperations:number};
const stageEvent = {attemptId:stage,jobId:stage,runId:stage,type:stage,sourceId:stage,status:stage,reasonCode:stage,startedAt:time,finishedAt:time,provider:stage,model:stage,reservedUsd:number,costUsd:number,credits:number};
const dailyError = {day,p1:number,p2:number,observedAt:time};
const dailyFunnel = {day,received:number,accepted:number,fulltext:number,structured:number,roles:number,published:number};
const funnelStage = {received:number,accepted:number,fulltext:number,structured:number,roles:number,published:number};
const EMPTY = Object.freeze({});
// Эти списки — единственный источник разрешённых полей. Произвольный payload,
// result_json, message, URL, keyMask, имя компании, название вакансии сюда не входят.
const SCHEMA = Object.freeze({
  scheduler_overview:{
    filters:EMPTY,summary:{enabled:bool,cloudStatus:stage,lastSignalAt:time,lastSuccessAt:time,nextSignalAt:time,counts,statistics:{sources:number,found:number,accepted:number,imported:number},spending:spent},items:EMPTY,
  },
  scheduler_queue:{
    filters:{status:stage,type:stage,sourceId:stage,searchApplied:bool},
    summary:{totalMatched:number,counts},
    items:{jobId:stage,type:stage,sourceId:stage,companyId:number,agencyId:number,candidateId:number,status:stage,priority:number,createdAt:time,updatedAt:time,runAfter:time,attempts:number,maxAttempts:number,reasonCode:stage},
    sortTime:'createdAt',sortId:'jobId',
  },
  scheduler_runs:{
    filters:{status:stage,trigger:stage},summary:{totalMatched:number},
    items:{runId:stage,trigger:stage,status:stage,startedAt:time,finishedAt:time,ready:number,processed:number,succeeded:number,errors:number,remaining:number,reason:stage},
    sortTime:'startedAt',sortId:'runId',
  },
  scheduler_attempts:{
    filters:{type:stage,provider:stage},summary:{totalMatched:number,...spent},
    items:{attemptId:stage,jobId:stage,runId:stage,type:stage,sourceId:stage,status:stage,reasonCode:stage,provider:stage,model:stage,startedAt:time,finishedAt:time,reservedUsd:number,costUsd:number,credits:number},
    sortTime:'startedAt',sortId:'attemptId',
  },
  ai_spending:{
    filters:{provider:stage,model:stage,type:stage},
    summary:{
      ...spent,
      dayBudgetUsd:number,monthBudgetUsd:number,dayStart:time,monthStart:time,
      dayConfirmedUsd:number,dayUnknownReservedUsd:number,dayUpperEstimateUsd:number,
      monthConfirmedUsd:number,monthUnknownReservedUsd:number,monthUpperEstimateUsd:number,
      monthUnknownCostOperations:number,monthUnpricedOperations:number,
      modelBreakdownAvailable:bool,duplicateAICallsAvailable:bool,
      providers:{openai:spent,openrouter:spent},
    },
    items:stageEvent,sortTime:'startedAt',sortId:'attemptId',
    breakdown:{provider:stage,model:stage,type:stage,...spent,unpricedOperations:number},
  },
  apify_usage:{
    filters:{profile:stage},
    // Provider usage и собственный ledger пересекаются. Профили не суммируются
    // между собой без проверенной идентичности аккаунтов/периодов.
    summary:{
      providerLimitMode:bool,activeOnly:bool,operationHistoryAvailable:bool,
      actorStartCountAvailable:bool,reconcileReadCountAvailable:bool,
      profiles:{
        free:{providerUsedUsd:number,ownConfirmedUsd:number,readReserveUsd:number,unknownReserveUsd:number,unknownRuns:number,limitUsd:number,remainingUsd:number,periodStart:time,periodEnd:time,checkedAt:time},
        starter:{providerUsedUsd:number,ownConfirmedUsd:number,readReserveUsd:number,unknownReserveUsd:number,unknownRuns:number,limitUsd:number,remainingUsd:number,periodStart:time,periodEnd:time,checkedAt:time},
      },
    },
    items:{operationId:stage,profile:stage,state:stage,createdAt:time,updatedAt:time,chargedUsd:number,reservedUsd:number,errorCode:stage,hasProviderRunId:bool,startOutcomeKnown:bool},sortTime:'createdAt',sortId:'operationId',
  },
  firecrawl_usage:{
    filters:{mode:stage},
    summary:{mode:stage,limitsCredits:number,providerPlanCredits:number,providerRemainingCredits:number,providerUsedEstimateCredits:number,reportedCredits:number,estimatedCredits:number,reservedCredits:number,effectiveUsedCredits:number,remainingCredits:number,periodStart:time,periodEnd:time,checkedAt:time,fresh:bool,budgetAvailable:bool,operationHistoryAvailable:bool,status:stage},
    items:EMPTY,
  },
  parser_issues:{
    filters:{priority:number,sourceId:stage},
    summary:{activeCounts:{p1:number,p2:number},historicalFailed:number,sampleLimited:bool,observedDays:number,days:number,average:{p1:number,p2:number},todayPeak:{p1:number,p2:number},lastMeasuredAt:time},
    items:{issueId:stage,priority:number,status:stage,reasonCode:stage,type:stage,sourceId:stage,observedAt:time,jobId:stage},
    daily:dailyError,sortTime:'observedAt',sortId:'issueId',
  },
  parser_funnel:{
    filters:{sourceId:stage,parser:stage},
    summary:{from:day,to:day,days:number,totals:funnelStage,averages:funnelStage,cohort:{...funnelStage,rejected:number,error:number,waiting:number,created:number,linked:number,undatedPublished:number}},
    items:EMPTY,daily:dailyFunnel,
  },
});
export const DEBUG_JSON_SECTIONS = Object.freeze(Object.keys(SCHEMA));

function isRecord(value) {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}
const DENY = /(?:bearer|authorization|cookie|secret|token|password|session|api[_-]?key|sk-[a-z0-9]|apify_api_|@|https?:|\\|[?=])/i;
function projectScalar(raw, kind) {
  if (raw === null) return null;
  if (kind === bool) return typeof raw === 'boolean' ? raw : undefined;
  if (kind === number) return typeof raw === 'number' && Number.isFinite(raw) ? raw : undefined;
  if (kind === day) return typeof raw === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(raw) && !Number.isNaN(Date.parse(raw+'T00:00:00Z')) ? raw : undefined;
  if (kind === time) {
    if (typeof raw !== 'number' && typeof raw !== 'string') return undefined;
    if (typeof raw === 'string' && !/(?:Z|[+-]\d{2}:\d{2})$/.test(raw)) return undefined;
    const ms = typeof raw === 'number' ? raw : Date.parse(raw);
    return Number.isFinite(ms) && ms >= 0 && ms < 253402300800000 ? new Date(ms).toISOString() : undefined;
  }
  if (kind === stage) {
    return typeof raw === 'string' && raw.length <= 96 && /^[a-zA-Z0-9][a-zA-Z0-9_.:/-]*$/.test(raw) && !DENY.test(raw) ? raw : undefined;
  }
  return undefined;
}
function project(input, schema) {
  const out = {};
  if (!isRecord(input)) return out;
  for (const [key, spec] of Object.entries(schema)) {
    if (!Object.hasOwn(input, key)) continue;
    const raw = input[key];
    if (typeof spec === 'string') {
      const value = projectScalar(raw, spec);
      if (value !== undefined) out[key] = value;
    } else if (isRecord(spec)) {
      if (raw === null) out[key] = null;
      else if (isRecord(raw)) out[key] = project(raw, spec);
    }
  }
  return out;
}
function milliseconds(item, field) {
  const raw = isRecord(item) && item[field];
  const ms = typeof raw === 'number' ? raw : typeof raw === 'string' ? Date.parse(raw) : NaN;
  return Number.isFinite(ms) ? ms : -Infinity;
}
function listProjection(source, config) {
  const items = source.items ?? [];
  if (!Array.isArray(items) || items.some(x=>!isRecord(x))) throw new Error('invalid_debug_items');
  const total = source.total ?? items.length;
  if (!Number.isSafeInteger(total) || total < items.length || total < 0) throw new Error('invalid_debug_total');
  const coverage = source.coverage ?? 'complete';
  if (coverage !== 'complete' && coverage !== 'server_latest') throw new Error('invalid_debug_coverage');
  // Пагинированная текущая страница не является десятью последними по всей БД.
  if (coverage === 'complete' && total !== items.length) throw new Error('incomplete_debug_source');
  if (coverage === 'server_latest' && items.length < Math.min(total, DEBUG_JSON_EVENT_LIMIT)) throw new Error('incomplete_debug_latest');
  const timeField = config.sortTime || 'startedAt',idField = config.sortId || 'jobId';
  const sorted = items.map((value,index)=>({value,index})).sort((a,b)=>
    milliseconds(b.value,timeField)-milliseconds(a.value,timeField) ||
    (String(a.value[idField]??'') < String(b.value[idField]??'') ? 1 : String(a.value[idField]??'') > String(b.value[idField]??'') ? -1 : 0) || a.index-b.index);
  const kept = sorted.slice(0,DEBUG_JSON_EVENT_LIMIT).map(x=>project(x.value,config.items));
  return {
    total,returned:kept.length,limit:DEBUG_JSON_EVENT_LIMIT,
    truncated:total > kept.length,sort:{field:timeField,direction:'desc',tieBreak:idField+' desc'},
    coverage,items:kept,
  };
}
export function buildDebugJSON(input) {
  if (!isRecord(input) || typeof input.section !== 'string' || !Object.hasOwn(SCHEMA,input.section)) throw new Error('invalid_debug_section');
  const config = SCHEMA[input.section];
  const capturedAt = projectScalar(input.capturedAt ?? new Date().toISOString(),time);
  if (!capturedAt) throw new Error('invalid_debug_timestamp');
  const output = {
    schemaVersion:DEBUG_JSON_SCHEMA_VERSION,
    section:input.section,
    capturedAt,timezone:'Europe/Moscow',
    available:input.available !== false,
    filters:project(input.filters,config.filters),
    // Недоступный endpoint не может подтверждать старые данные из кэша.
    summary:input.available === false ? {} : project(input.summary,config.summary),
  };
  if (input.available === false) {
    output.errorCode = projectScalar(input.errorCode,stage) || 'unavailable';
    if (input.lastSuccessfulAt !== undefined) output.lastSuccessfulAt = projectScalar(input.lastSuccessfulAt,time) ?? null;
  }
  const dataAsOf = projectScalar(input.dataAsOf ?? null,time);
  output.dataAsOf = dataAsOf ?? null;
  if (typeof input.appVersion === 'string' && /^[a-f0-9]{7,40}$/i.test(input.appVersion)) output.appVersion = input.appVersion;
  if (Object.keys(config.items).length && input.available !== false && input.items !== undefined) {
    output.recentItems = listProjection(input,config);
  }
  if (config.breakdown && input.available !== false && input.breakdown !== undefined) {
    if (!Array.isArray(input.breakdown) || input.breakdown.some(x=>!isRecord(x))) throw new Error('invalid_debug_breakdown');
    // Только десять самых затратных сочетаний provider/model/stage.
    // Полная сумма остаётся в summary, без переумножения на найденные вакансии.
    const limited=input.breakdown.slice(0,DEBUG_JSON_EVENT_LIMIT);
    output.breakdown={total:input.breakdown.length,returned:limited.length,
      truncated:input.breakdown.length>limited.length,
      items:limited.map(x=>project(x,config.breakdown))};
  }
  if (config.daily && input.available !== false && input.daily !== undefined) {
    if (!Array.isArray(input.daily) || input.daily.length !== 30 || input.daily.some(x=>!isRecord(x))) throw new Error('invalid_debug_daily');
    output.daily = input.daily.map(x=>project(x,config.daily)).sort((a,b)=>String(a.day).localeCompare(String(b.day)));
  }
  return output;
}
export function serializeDebugJSON(input) {
  const output = buildDebugJSON(input);
  let text = JSON.stringify(output,null,2);
  if (new TextEncoder().encode(text).length > DEBUG_JSON_MAX_BYTES && output.recentItems) {
    while (output.recentItems.items.length && new TextEncoder().encode(text).length > DEBUG_JSON_MAX_BYTES) {
      output.recentItems.items.pop();
      output.recentItems.returned = output.recentItems.items.length;
      output.recentItems.truncated = output.recentItems.total > output.recentItems.returned;
      text = JSON.stringify(output,null,2);
    }
  }
  if (new TextEncoder().encode(text).length > DEBUG_JSON_MAX_BYTES) throw new Error('debug_export_too_large');
  return text;
}
