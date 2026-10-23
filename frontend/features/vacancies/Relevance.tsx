import {useSyncExternalStore} from 'react';
import {mutate,onCacheEvent} from '../../api/client';
import type {VacancyRelevanceResult} from '../../../shared/contracts';

interface State {busy:boolean;error?:string;}
const idle:State={busy:false},states=new Map<number,State>(),listeners=new Set<()=>void>();
const notify=()=>listeners.forEach(fn=>fn());
onCacheEvent(event=>{if(event.type==='reset'){states.clear();notify();}});
const subscribe=(listener:()=>void)=>{listeners.add(listener);return ()=>{listeners.delete(listener);};};
async function recalculate(id:number){
 if(states.get(id)?.busy)return;
 const running:State={busy:true};states.set(id,running);notify();
 try{
  await mutate<VacancyRelevanceResult>('/api/vacancies/'+id+'/relevance','POST');
  if(states.get(id)===running){states.delete(id);notify();}
 }catch(error){if(states.get(id)===running){states.set(id,{busy:false,error:error instanceof Error?error.message:'Не удалось рассчитать релевантность.'});notify();}}
}
export function VacancyRelevance({id,value,title}:{id:number;value:number|null;title:string}){
 const state=useSyncExternalStore(subscribe,()=>states.get(id)||idle,()=>idle);
 return <div className="vacancy-relevance-control">
  <button type="button" className={'vacancy-relevance vacancy-relevance-button'+(value==null?' empty':'')}
   disabled={state.busy} aria-busy={state.busy} aria-label={(state.busy?'Расчёт релевантности: ':'Пересчитать релевантность: ')+title}
   title={state.busy?'Рассчитываем релевантность…':'Нажмите, чтобы пересчитать релевантность'} onClick={()=>void recalculate(id)}>
   {state.busy?'…':value==null?'—':value+'%'}
  </button>
  {state.error&&<span className="vacancy-relevance-error" role="alert">{state.error}</span>}
 </div>;
}
