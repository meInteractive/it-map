import {useEffect,useRef,useState} from 'react';
import {useApi,onCacheEvent} from '../../api/client';
import {composeAllDiagnostics} from '../../../shared/all-diagnostics-debug.mjs';
import {schedulerOverviewSnapshot,schedulerQueueSnapshot,schedulerRunsSnapshot,schedulerAttemptsSnapshot,type QueueDebugResponse} from '../../../shared/scheduler-debug.mjs';
import {aiSpendingSnapshot,type AISpendingDebugReport} from '../../../shared/ai-spending-debug.mjs';
import {apifyUsageSnapshot,firecrawlUsageSnapshot} from '../../../shared/provider-debug.mjs';
import {parserIssuesSnapshot,parserFunnelSnapshot} from '../../../shared/parser-statistics-debug.mjs';
import type {IssuesReport} from './ParserIssues';
import type {ParserFunnelReport} from '../../../shared/parser-funnel';
import type {DebugJSONInput} from '../../../shared/debug-json.mjs';

type Snapshot=Omit<DebugJSONInput,'section'>;
type ReadResult<T>={data?:T;error?:Error;loading:boolean};
interface Props {dashboard:any;dashboardError?:Error;isAuthorized:()=>boolean;}
const unavailable=(code:string):Snapshot=>({available:false,errorCode:code});
function safeRead<T>(query:ReadResult<T>,convert:(value:T)=>Snapshot,code:string):Snapshot {
 if(!query.data||query.error)return unavailable(code);
 try {return convert(query.data);} catch {return unavailable(code+'_invalid');}
}

// Все запросы — к сохранённой диагностике внутри it-map. POST не используется.
// Данные загружаются до клика, чтобы не терять право браузера на clipboard.
export default function CopyAllDiagnostics({dashboard,dashboardError,isAuthorized}:Props) {
 const queue=useApi<QueueDebugResponse>('/api/scheduler/queue/debug?page=1&status=&type=&search=');
 const ai=useApi<AISpendingDebugReport>('/api/scheduler/ai-spending/debug');
 const apify=useApi<Parameters<typeof apifyUsageSnapshot>[0]>('/api/apify/usage');
 const fireSettings=useApi<{mode:'free'|'hobby'}>('/api/firecrawl/settings');
 const mode=fireSettings.data?.mode;
 const firecrawl=useApi<Parameters<typeof firecrawlUsageSnapshot>[0]>(mode?'/api/firecrawl/usage?mode='+mode:null);
 const issues=useApi<IssuesReport>('/api/scheduler/issues');
 const funnel=useApi<ParserFunnelReport>('/api/scheduler/funnel?sourceId=all&parser=all');
 const [status,setStatus]=useState(''),[busy,setBusy]=useState(false);
 const inFlight=useRef(false),epoch=useRef(0);
 useEffect(()=>onCacheEvent(e=>{
   if(e.type==='reset'){epoch.current++;setStatus('');}
 }),[]);
 const loading=[queue,ai,apify,fireSettings,firecrawl,issues,funnel]
   .some(q=>q.loading&&!q.data&&!q.error);
 const copy=async()=>{
   if(inFlight.current||loading||!isAuthorized())return;
   inFlight.current=true;setBusy(true);setStatus('');
   const initialEpoch=epoch.current;
   try {
     const assertSession=()=>{
       if(initialEpoch!==epoch.current||!isAuthorized())throw new Error('session_changed');
     };
     assertSession();
     const snapshots={
       schedulerOverview:dashboard&&!dashboardError?schedulerOverviewSnapshot(dashboard):unavailable('scheduler_unavailable'),
       queue:safeRead(queue,v=>({
         ...schedulerQueueSnapshot(v,{status:'all',type:'all',searchApplied:false}),
         filters:{status:'all',type:'all',searchApplied:false},
       }),'queue_unavailable'),
       runs:dashboard&&!dashboardError?schedulerRunsSnapshot(dashboard):unavailable('runs_unavailable'),
       attempts:dashboard&&!dashboardError?schedulerAttemptsSnapshot(dashboard):unavailable('attempts_unavailable'),
       aiSpending:safeRead(ai,v=>aiSpendingSnapshot(v,{
         dayBudgetUsd:dashboard.settings.dayBudgetUsd,
         monthBudgetUsd:dashboard.settings.monthBudgetUsd,
       }),'ai_spending_unavailable'),
       apify:safeRead(apify,apifyUsageSnapshot,'apify_unavailable'),
       firecrawl:mode?safeRead(firecrawl,firecrawlUsageSnapshot,'firecrawl_unavailable'):
         unavailable('firecrawl_mode_unavailable'),
       issues:safeRead(issues,parserIssuesSnapshot,'issues_unavailable'),
       funnel:safeRead(funnel,parserFunnelSnapshot,'funnel_unavailable'),
     };
     // Финальная проверка после чтения React state и до обращения к clipboard.
     assertSession();
     const json=composeAllDiagnostics(snapshots,new Date().toISOString());
     assertSession();
     if(typeof navigator.clipboard?.writeText!=='function')throw new Error('clipboard_unavailable');
     await navigator.clipboard.writeText(json);
     assertSession();
     setStatus('Вся диагностика скопирована');
   }catch{
     if(initialEpoch===epoch.current)setStatus('Не удалось скопировать всю диагностику');
   }finally{inFlight.current=false;setBusy(false);}
 };
 return <div className="scheduler-stack">
   <div className="scheduler-actions">
     <button className="settings-button secondary" type="button"
       disabled={!isAuthorized()||busy||loading} aria-busy={busy} onClick={copy}>
       {loading?'Подготовка диагностики…':'Копировать всю диагностику'}
     </button>
     <span role="status" aria-live="polite" className="resume-hint">{status}</span>
   </div>
   <p className="resume-hint">Скопированный JSON содержит техническую диагностику; перед отправкой третьим лицам проверьте содержимое. Экспорт включает только разрешённые поля. Очередь выгружается без фильтров, остальные разделы — по текущему серверному состоянию.</p>
 </div>;
}
