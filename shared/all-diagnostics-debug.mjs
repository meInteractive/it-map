import {serializeDebugJSON,DEBUG_JSON_MAX_BYTES,DEBUG_JSON_SCHEMA_VERSION} from './debug-json.mjs';

// Сборка опирается только на существующие белые списки отдельных экспортов.
// На вход не передаются сырые дампы и произвольные данные из БД.
export const ALL_DIAGNOSTICS_SECTIONS=Object.freeze({
  schedulerOverview:'scheduler_overview',
  queue:'scheduler_queue',
  runs:'scheduler_runs',
  attempts:'scheduler_attempts',
  aiSpending:'ai_spending',
  apify:'apify_usage',
  firecrawl:'firecrawl_usage',
  issues:'parser_issues',
  funnel:'parser_funnel',
});

const sizeOf=value=>new TextEncoder().encode(JSON.stringify(value,null,2)).length;
function sectionSnapshot(section,source,capturedAt) {
  const input=source??{available:false,errorCode:'section_unavailable'};
  let safe;
  try {
    safe=JSON.parse(serializeDebugJSON({section,capturedAt,...input}));
  } catch {
    safe=JSON.parse(serializeDebugJSON({section,capturedAt,available:false,errorCode:'section_invalid'}));
  }
  const asOf=safe.dataAsOf ? Date.parse(safe.dataAsOf) : NaN;
  // Пять минут — только эвристика UI, не срок годности данных провайдера.
  safe.stale=Number.isFinite(asOf) ? Math.abs(Date.parse(capturedAt)-asOf)>300000 : null;
  safe.completeness=!safe.available?'unavailable':
    safe.recentItems?.truncated||safe.breakdown?.truncated?'partial':'complete';
  return safe;
}
function trimOne(sections) {
  for (const value of Object.values(sections)) {
    if (value?.recentItems?.items?.length) {
      value.recentItems.items.pop();
      value.recentItems.returned=value.recentItems.items.length;
      value.recentItems.truncated=value.recentItems.total>value.recentItems.returned;
      value.completeness='partial';
      return true;
    }
  }
  for (const value of Object.values(sections)) {
    const jobs=value?.summary?.p2Breakdown?.recentJobs;
    if (Array.isArray(jobs)&&jobs.length) {
      jobs.pop();
      value.summary.p2Breakdown.truncated=true;
      value.completeness='partial';
      return true;
    }
  }
  for (const value of Object.values(sections)) {
    if (value?.breakdown?.items?.length) {
      value.breakdown.items.pop();
      value.breakdown.returned=value.breakdown.items.length;
      value.breakdown.truncated=true;
      value.completeness='partial';
      return true;
    }
  }
  return false;
}

// Сборка не делает сеть и оставляет все 30 дневных точек даже при усечении списков.
export function composeAllDiagnostics(input,capturedAt=new Date().toISOString(),appVersion=null) {
  const valid=new Date(capturedAt).toISOString();
  const sections={};
  for(const [name,section] of Object.entries(ALL_DIAGNOSTICS_SECTIONS)) {
    sections[name]=sectionSnapshot(section,input?.[name],valid);
  }
  const envelope={schemaVersion:DEBUG_JSON_SCHEMA_VERSION,capturedAt:valid,
    timezone:'Europe/Moscow',appVersion:typeof appVersion==='string'&&/^[a-f0-9]{7,40}$/i.test(appVersion)?appVersion:null,
    sections};
  while (sizeOf(envelope)>DEBUG_JSON_MAX_BYTES && trimOne(sections)) {}
  if(sizeOf(envelope)>DEBUG_JSON_MAX_BYTES)throw new Error('debug_export_too_large');
  return JSON.stringify(envelope,null,2);
}
