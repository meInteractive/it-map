import {boundGeneration} from '../repositories/generation-bound-db.js';
import {operationErrorCode} from '../services/operation-error.js';

const COOLDOWN_MS=15*60*1000;
export const structuredTask=task=>['vacancy_extraction','vacancy_role'].includes(task);
// Reuse the existing timing store with a task/epoch/key-revision namespace.
// Letter timings stay under their existing plain model IDs. No content is logged.
const prefix=(db,task,revision)=>`structured:${task}:${boundGeneration(db)}:${revision}:`;
export async function rankStructuredModels(db,task,models,revision,now=Date.now()){
 const scope=prefix(db,task,revision);
 const rows=(await db.prepare('SELECT * FROM openrouter_model_metrics WHERE substr(model_id,1,length(?))=?').bind(scope,scope).all()).results||[];
 const metrics=new Map(rows.map(r=>[r.model_id.slice(scope.length),r]));
 const details=models.map((model,index)=>{
  const row=metrics.get(model.id),failed=Date.parse(row?.last_failure_at||''),success=Date.parse(row?.last_success_at||'');
  const cooling=Number.isFinite(failed)&&now-failed<COOLDOWN_MS&&failed>=(Number.isFinite(success)?success:0);
  const latency=row?.success_count>=2?row.total_duration_ms/row.success_count:60000;
  return {model,index,cooling,failed,latency};
 });
 // If every available model is cooling, try the least recently failed one on
 // the queue's bounded retry. Never invent a model or enable paid fallback.
 details.sort((a,b)=>Number(a.cooling)-Number(b.cooling)||(a.cooling&&b.cooling?a.failed-b.failed:0)||a.latency-b.latency||a.index-b.index);
 return details.map(r=>r.model);
}
export async function recordStructuredModel(db,task,model,revision,elapsed,error){
 const code=error?operationErrorCode(error):null;
 if(error&&(error.name==='AbortError'||['restored','scheduler_changed','job_changed','detail_changed','generation_lease_lost','scheduler_budget','budget_unknown','missing_key','invalid_key','decrypt','source_scope','scope'].includes(code)))return;
 const ok=!error,at=new Date().toISOString(),duration=Math.min(900000,Math.max(0,Math.round(elapsed)));
 await db.prepare(`INSERT INTO openrouter_model_metrics(model_id,success_count,total_duration_ms,last_duration_ms,last_success_at,failure_count,last_failure_duration_ms,last_failure_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(model_id) DO UPDATE SET
 success_count=success_count+excluded.success_count,total_duration_ms=total_duration_ms+excluded.total_duration_ms,
 last_duration_ms=coalesce(excluded.last_duration_ms,last_duration_ms),last_success_at=coalesce(excluded.last_success_at,last_success_at),
 failure_count=failure_count+excluded.failure_count,last_failure_duration_ms=coalesce(excluded.last_failure_duration_ms,last_failure_duration_ms),last_failure_at=CASE WHEN excluded.success_count=1 THEN NULL ELSE coalesce(excluded.last_failure_at,last_failure_at) END`)
 .bind(prefix(db,task,revision)+model,ok?1:0,ok?duration:0,ok?duration:null,ok?at:null,ok?0:1,ok?null:duration,ok?null:at).run();
}
