import {VACANCY_SOURCE_META} from '../constants.js';
import {candidateDb,candidateClassificationContext,candidateStatusProjection} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {PARSER_EXPECTED_WAITS} from './parser-issues.js';
import {funnelStages} from '../../shared/parser-funnel.ts';

export const FUNNEL_PARSERS=[{id:'apify',name:'Apify'},{id:'firecrawl',name:'Firecrawl'},{id:'http',name:'Прямой HTTP'},{id:'unknown',name:'Не определён'}];
const day=now=>new Date(now+10800000).toISOString().slice(0,10);
const empty=()=>Object.fromEntries(funnelStages.map(s=>[s.id,0]));
export async function parserFunnel(database,{sourceId='all',parser='all',now=Date.now()}={}){
 const sourceOptions=[...VACANCY_SOURCE_META.map(([id,name])=>({id,name})),{id:'unknown',name:'Источник не указан'}];
 if(!['all',...sourceOptions.map(s=>s.id)].includes(sourceId)||!['all',...FUNNEL_PARSERS.map(p=>p.id)].includes(parser))throw Object.assign(new Error('Некорректный источник или парсер.'),{status:400});
 const db=await candidateDb(database),projection=candidateStatusProjection(await candidateClassificationContext(db));
 const keys=Array.from({length:30},(_,i)=>day(now-(29-i)*86400000)),from=keys[0],to=keys.at(-1);
 // Milestone timestamps are first successful facts, not retry counts, external
 // job publication dates, or a later provider guessed from the HH URL.
 const sql=`WITH ${projection.cte}, origin AS (
 SELECT c.*,CASE WHEN i.candidate_token IS NOT NULL THEN COALESCE(i.source_id,'unknown') ELSE COALESCE(c.source_id,'unknown') END intake_source,COALESCE(i.parser,'unknown') intake_parser,
 COALESCE(i.received_at,c.first_seen_at) receivedAt
 FROM vacancy_candidates c LEFT JOIN parser_candidate_intakes i ON i.candidate_token=c.creation_token
 WHERE (i.parser IN ('apify','firecrawl','http','unknown') OR (i.parser IS NULL AND c.acquisition_method IN ('site','firecrawl')))
 ), facts AS (
 SELECT c.id,c.main_vacancy_id,c.receivedAt,${projection.sql} processingStatus,
 (SELECT min(classified_at) FROM vacancy_classifications WHERE candidate_id=c.id AND decision='match') acceptedAt,
 (SELECT min(fetched_at) FROM vacancy_raw_snapshots WHERE candidate_id=c.id AND content_status='valid') fulltextAt,
 (SELECT min(processed_at) FROM vacancy_extractions WHERE candidate_id=c.id AND status='success') structuredAt,
 (SELECT min(processed_at) FROM vacancy_role_results WHERE candidate_id=c.id AND status='success' AND json_array_length(CASE WHEN json_valid(role_ids) THEN role_ids ELSE '[]' END)>0) rolesAt,
 (SELECT min(processed_at) FROM vacancy_import_operations WHERE candidate_id=c.id AND status='success' AND action IN ('create','update','unchanged')) publishedAt,
 EXISTS(SELECT 1 FROM vacancy_import_operations WHERE candidate_id=c.id AND status='success' AND action='create') created,
 EXISTS(SELECT 1 FROM scheduler_jobs j WHERE j.candidate_id=c.id AND j.generation=? AND j.status IN ('failed','pending')
  AND (j.status='failed' OR COALESCE(j.reason_code,'') NOT IN (SELECT value FROM json_each(?)))
  AND NOT EXISTS(SELECT 1 FROM scheduler_jobs n WHERE n.generation=j.generation AND n.candidate_id=j.candidate_id AND n.type=j.type AND n.status IN ('completed','running','pending') AND (n.updated_at>j.updated_at OR (n.updated_at=j.updated_at AND n.id>j.id)))) jobError
 FROM origin c WHERE (?='all' OR intake_source=?) AND (?='all' OR intake_parser=?)
 ), states AS (
 SELECT *,CASE WHEN publishedAt IS NOT NULL OR main_vacancy_id IS NOT NULL THEN 'published'
 WHEN processingStatus='rejected' THEN 'rejected'
 WHEN processingStatus='classification_error' OR jobError THEN 'error' ELSE 'waiting' END currentState FROM facts
 )`;
 const bindings=[...projection.bindings,boundGeneration(db),JSON.stringify(PARSER_EXPECTED_WAITS),sourceId,sourceId,parser,parser];
 const events=`${sql}, events AS (
 SELECT date(e.value,'+3 hours') day,e.key stage FROM states s JOIN json_each(json_object('received',receivedAt,'accepted',acceptedAt,'fulltext',fulltextAt,'structured',structuredAt,'roles',rolesAt,'published',publishedAt)) e
 WHERE e.value IS NOT NULL AND date(e.value,'+3 hours') BETWEEN ? AND ?
 ) SELECT day,stage,count(*) total FROM events GROUP BY day,stage`;
 const counts=funnelStages.map(s=>s.id==='received'?'count(*) received':`COALESCE(sum(${s.id==='published'?"currentState='published'":s.id+'At IS NOT NULL'}),0) ${s.id}`).join(',');
 const aggregate=`${sql} SELECT ${counts},
 COALESCE(sum(currentState='rejected'),0) rejected,COALESCE(sum(currentState='error'),0) error,COALESCE(sum(currentState='waiting'),0) waiting,
 COALESCE(sum(currentState='published' AND created=1),0) created,COALESCE(sum(publishedAt IS NOT NULL AND created=0),0) linked,
 COALESCE(sum(currentState='published' AND publishedAt IS NULL),0) undatedPublished
 FROM states WHERE date(receivedAt,'+3 hours') BETWEEN ? AND ?`;
 const result=await db.batch([db.prepare(events).bind(...bindings,from,to),db.prepare(aggregate).bind(...bindings,from,to)]);
 const daily=new Map(keys.map(day=>[day,{day,...empty()}]));
 for(const row of result[0].results||[])if(daily.has(row.day))daily.get(row.day)[row.stage]=row.total;
 const totals=empty();for(const row of daily.values())for(const s of funnelStages)totals[s.id]+=row[s.id];
 return {checkedAt:now,timezone:'Europe/Moscow',from,to,days:30,filters:{sourceId,parser},sourceOptions,parserOptions:FUNNEL_PARSERS,
 daily:[...daily.values()],totals,averages:Object.fromEntries(Object.entries(totals).map(([k,v])=>[k,v/30])),cohort:result[1].results[0]};
}
