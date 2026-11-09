import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';

const {default:ParserExpenseRepeats,ExpenseRepeatsView,isExpenseRepeatsReport}=
 await moduleFor('frontend/features/settings/ParserExpenseRepeats.tsx');
const old=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
const sample={
 checkedAt:Date.now(),timezone:'Europe/Moscow',periodDays:30,from:'2026-10-11',to:'2026-11-09',
 providerRepeatVerified:false,windowLimited:true,
 apify:{actorOperations:4,localReceipts:7,repeatedReceiptGroups:1,extraLocalReceipts:2,
  conflictingActorGroups:1,suspectedLaunchGroups:1,actorsInSuspectedGroups:2,
  missingActorIdentity:1,unknownStartReserveUsd:0.26},
 ai:{observedAttempts:14,repeatGroups:2,extraAttempts:3,stages:[
  {type:'classify_title',repeatGroups:1,extraAttempts:2,knownCostAttempts:2,
   knownLocalCostUsd:0.023,unknownCostAttempts:1,unknownReserveUsd:0.1,unpricedAttempts:0},
  {type:'extract_vacancy',repeatGroups:1,extraAttempts:1,knownCostAttempts:1,
   knownLocalCostUsd:0.019,unknownCostAttempts:0,unknownReserveUsd:0,unpricedAttempts:1}
 ]}
};
assert.equal(isExpenseRepeatsReport(sample),true);
for(const bad of [
 null,{}, {...sample,ai:null},{...sample,ai:{...sample.ai,stages:null}},
 {...sample,apify:{...sample.apify,missingActorIdentity:-1}},
 {...sample,apify:{...sample.apify,unknownStartReserveUsd:Infinity}},
 {...sample,providerRepeatVerified:true},
 {...sample,periodDays:12},
 {...sample,from:'2026-13-04'},
 {...sample,ai:{...sample.ai,stages:[{...sample.ai.stages[0],knownLocalCostUsd:NaN}]}},
 {...sample,ai:{...sample.ai,stages:[{...sample.ai.stages[0],type:'PRIVATE-ENTITY'}]}}
])assert.equal(isExpenseRepeatsReport(bad),false,'invalid diagnostic accepted');

const html=render(ExpenseRepeatsView,{report:sample});
for(const phrase of ['признаки повторов','не подтверждение повторной оплаты','повторные квитанции',
 'возможные повторные старты','Повторные ИИ-попытки','сверки','резерв']){
 assert.match(html,new RegExp(phrase,'i'));
}
assert.ok(!html.includes('NaN')&&!html.includes('Infinity')&&!html.includes('jobId'));
assert.equal((html.match(/<tbody>.*?<\/tbody>/s)[0].match(/<tr>/g)||[]).length,2);
const empty=render(ExpenseRepeatsView,{report:{...sample,ai:{observedAttempts:0,repeatGroups:0,extraAttempts:0,stages:[]},
 apify:{actorOperations:0,localReceipts:0,repeatedReceiptGroups:0,extraLocalReceipts:0,
 conflictingActorGroups:0,suspectedLaunchGroups:0,actorsInSuspectedGroups:0,
 missingActorIdentity:0,unknownStartReserveUsd:0}}});
assert.match(empty,/не обнаружено/);
let root,mode='ok',delayed=false;
const requests=[],pending=[];
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
 assert.ok(url.startsWith('/api/scheduler/expense-repeats?days='));
 assert.ok(!options.method||options.method==='GET','diagnostics must be read-only GET');
 const days=Number(new URL(url,'https://ci.invalid').searchParams.get('days'));
 requests.push(days);
 if(mode==='network')throw new Error('Синтетический сетевой сбой');
 if(mode==='broken')return Response.json({...sample,periodDays:days,apify:null});
 if(mode==='invalid-json')return new Response('{invalid',{status:200,headers:{'content-type':'application/json'}});
 const payload={...sample,periodDays:days,from:days===7?'2026-11-03':sample.from};
 if(delayed&&days===7)return new Promise(resolve=>pending.push({resolve,payload}));
 return Response.json(payload);
};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const view=()=>JSON.stringify(root.toJSON());
try{
 await act(async()=>{root=create(h(ParserExpenseRepeats));});
 await flush();await flush();
 const period=()=>root.root.findByProps({'aria-label':'Период диагностики повторов'});
 const refresh=()=>root.root.findAllByType('button').find(b=>b.props.children==='Обновить');
 assert.equal(period().props.value,30);
 assert.deepEqual(requests,[30]);
 assert.match(view(),/возможные повторные старты/);

 await act(async()=>period().props.onChange({target:{value:'7'}}));
 await flush();await flush();
 assert.equal(requests.at(-1),7);
 assert.equal(period().props.value,7);

 mode='broken';
 await act(async()=>period().props.onChange({target:{value:'30'}}));
 await flush();await flush();
 assert.match(view(),/Некорректный ответ диагностики повторов/);
 assert.doesNotMatch(view(),/Apify · повторные квитанции/);
 mode='ok';
 await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.match(view(),/возможные повторные старты/);

 mode='network';
 await act(async()=>period().props.onChange({target:{value:'7'}}));
 await flush();await flush();
 assert.match(view(),/Синтетический сетевой сбой/);
 mode='ok';
 await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.match(view(),/Повторные ИИ-попытки/);

 // Delayed old period cannot replace the currently selected 30-day report.
 delayed=true;
 await act(async()=>refresh().props.onClick());await flush();
 assert.equal(pending.length,1);
 delayed=false;
 await act(async()=>period().props.onChange({target:{value:'30'}}));
 await flush();await flush();
 assert.equal(period().props.value,30);
 assert.match(view(),/возможные повторные старты/);
 await act(async()=>pending[0].resolve(Response.json(pending[0].payload)));await flush();
 assert.equal(period().props.value,30);
 assert.match(view(),/возможные повторные старты/);
 assert.ok(requests.every(d=>d===7||d===30));
 console.log('GO-434 UI: data-contract guards, evidence disclaimers, periods, invalid payload, network retry, GET-only and delayed filter passed.');
}finally{
 if(root)await act(async()=>root.unmount());
 Object.assign(globalThis,old);
}
