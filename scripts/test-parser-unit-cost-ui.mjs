import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';

const {default:ParserUnitCost,UnitCostView,isUnitCostReport}=await moduleFor('frontend/features/settings/ParserUnitCost.tsx');
const previous=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
const days=Array.from({length:30},(_,i)=>new Date(Date.UTC(2026,9,11+i)).toISOString().slice(0,10));
const ledger={confirmedUsd:0.5,confirmedOperations:2,unknownReservedUsd:0.26,unknownOperations:1,
 unpricedOperations:0,conflictingOperations:0,unavailableForSource:false};
const lineage={attempts:6,withJob:5,withCandidate:3,withMainVacancy:3,withMatchingCreate:2,
 missingJob:1,missingCandidate:1,missingMainVacancy:0,sourceMismatch:1,
 knownUsdWithMainLink:0.3,knownUsdWithoutMainLink:0,verifiedProviderAttribution:false};
const report={
 checkedAt:Date.now(),timezone:'Europe/Moscow',from:days[0],to:days.at(-1),periodDays:30,
 sourceId:'all',sourceOptions:[{id:'hh',name:'HH'},{id:'companySite',name:'Сайт компании'},{id:'unknown',name:'Не указан'}],
 newUniqueVacancies:2,undatedCreates:0,
 apify:ledger,ai:{...ledger,confirmedUsd:0.3,unknownReservedUsd:0.5},
 aiLineage:lineage,
 firecrawlCredits:2.5,unknownCreditRows:1,totalConfirmedUsd:0.8,partialPeriodRatioUsd:0.4,
 calendarProxy:true,incompleteCosts:true,daily:days.map((day,i)=>({
  day,newVacancies:i===28||i===29?1:0,aiUsd:i===29?0.3:0,apifyUsd:i===29?0.5:0,observedCredits:i===29?2.5:0
 }))
};
assert.equal(isUnitCostReport(report),true,'synthetic server report must be valid');
for(const bad of [
 null,{}, {...report,aiLineage:null},
 {...report,sourceOptions:[{id:'hh'}]},
 {...report,totalConfirmedUsd:-1},
 {...report,totalConfirmedUsd:9},
 {...report,partialPeriodRatioUsd:-1},
 {...report,partialPeriodRatioUsd:0.1},
 {...report,daily:report.daily.slice(1)},
 {...report,daily:report.daily.map((d,i)=>i===2?{...d,day:'bad-date'}:d)},
 {...report,daily:report.daily.map((d,i)=>i===29?{...d,aiUsd:NaN}:d)},
 {...report,daily:report.daily.map((d,i)=>i===29?{...d,newVacancies:3}:d)},
 {...report,ai:{...ledger,confirmedUsd:Infinity}},
 {...report,firecrawlCredits:-2}
])assert.equal(isUnitCostReport(bad),false,'invalid API report was accepted');
const tiny={...report,ai:{...ledger,confirmedUsd:0.000001},apify:{...ledger,confirmedUsd:0},
 totalConfirmedUsd:0.000001,partialPeriodRatioUsd:0.0000005,
 daily:report.daily.map((d,i)=>({...d,aiUsd:i===29?0.000001:0,apifyUsd:0}))};
assert.equal(isUnitCostReport(tiny),true);
assert.match(render(UnitCostView,{report:tiny}),/менее \$0,00001/);
assert.match(render(UnitCostView,{report:tiny}),/height="148"/,'tiny positive spend must remain visible in chart');
const html=render(UnitCostView,{report});
assert.match(html,/Прослеживаемость расходов ИИ/);
assert.match(html,/С кандидатом/);
assert.match(html,/Разрывы и расхождения связей/);
assert.match(html,/не распределены/);
assert.match(html,/matching create: 2/);
assert.ok(!html.includes('NaN')&&!html.includes('Infinity'));
assert.equal((html.match(/<tbody>.*?<\/tbody>/s)[0].match(/<tr>/g)||[]).length,30);
const empty={...report,newUniqueVacancies:0,partialPeriodRatioUsd:null,totalConfirmedUsd:0,
 apify:{...report.apify,confirmedUsd:0},ai:{...report.ai,confirmedUsd:0},
 aiLineage:{...lineage,attempts:0,withJob:0,withCandidate:0,withMainVacancy:0,withMatchingCreate:0,
  knownUsdWithMainLink:0,knownUsdWithoutMainLink:0}};
const noRatio=render(UnitCostView,{report:empty});
assert.match(noRatio,/Нет значения/);
assert.match(noRatio,/Деление на ноль не выполняется/);
assert.match(noRatio,/Нет операций/);
assert.ok(!noRatio.includes('NaN')&&!noRatio.includes('Infinity'));
let root;
const requests=[],pending=[];
let mode='ok',delayedSource='';
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
 assert.ok(!options.method||options.method==='GET','unit-cost panel must never POST');
 assert.ok(url.startsWith('/api/scheduler/unit-cost?'));
 const query=new URL(url,'https://ci.invalid').searchParams;
 const days=Number(query.get('days')),source=query.get('sourceId');
 requests.push({days,source});
 if(mode==='network')throw new Error('Синтетический сбой сети');
 if(mode==='malformed')return new Response('{malformed',{status:200,headers:{'content-type':'application/json'}});
 const daily=report.daily.slice(-days).map(d=>({...d,
  newVacancies:source==='hh'&&d.day===report.daily.at(-2).day?0:d.newVacancies,
  apifyUsd:source==='all'?d.apifyUsd:0}));
 const newUniqueVacancies=daily.reduce((sum,d)=>sum+d.newVacancies,0);
 const aiUsd=0.3,apifyUsd=source==='all'?0.5:0;
 const result={...report,periodDays:days,sourceId:source,from:daily[0].day,to:daily.at(-1).day,
  apify:source==='all'?ledger:{...ledger,confirmedUsd:0,confirmedOperations:0,unavailableForSource:true},
  ai:{...ledger,confirmedUsd:aiUsd},
  totalConfirmedUsd:aiUsd+apifyUsd,newUniqueVacancies,
  partialPeriodRatioUsd:newUniqueVacancies?(aiUsd+apifyUsd)/newUniqueVacancies:null,daily};
 if(mode==='incomplete')return Response.json({...result,aiLineage:null});
 if(source===delayedSource)return new Promise(resolve=>pending.push({resolve,result}));
 return Response.json(result);
};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
try {
 await act(async()=>{root=create(h(ParserUnitCost));});
 await flush();await flush();
 const period=()=>root.root.findByProps({'aria-label':'Период расчёта стоимости'});
 const source=()=>root.root.findByProps({'aria-label':'Источник расчёта стоимости'});
 assert.equal(period().props.value,30);
 assert.equal(source().props.value,'all');
 await act(async()=>period().props.onChange({target:{value:'7'}}));await flush();await flush();
 assert.deepEqual(requests.at(-1),{days:7,source:'all'});
 await act(async()=>source().props.onChange({target:{value:'hh'}}));await flush();await flush();
 assert.deepEqual(requests.at(-1),{days:7,source:'hh'});
 assert.match(JSON.stringify(root.toJSON()),/Не распределено/);
 assert.ok(requests.every(x=>x.days===7||x.days===30));
 // A structurally broken report must never be presented as a financial zero.
 mode='incomplete';
 await act(async()=>source().props.onChange({target:{value:'companySite'}}));await flush();await flush();
 assert.match(JSON.stringify(root.toJSON()),/Некорректный ответ статистики/);
 assert.doesNotMatch(JSON.stringify(root.toJSON()),/Частичный ориентир на вакансию/);
 mode='ok';
 await act(async()=>root.root.findAllByType('button').find(b=>b.props.children==='Обновить').props.onClick());await flush();await flush();
 assert.match(JSON.stringify(root.toJSON()),/Частичный ориентир на вакансию/);
 // An invalid JSON payload is also an error, not a valid empty report.
 mode='malformed';
 await act(async()=>source().props.onChange({target:{value:'unknown'}}));await flush();await flush();
 assert.match(JSON.stringify(root.toJSON()),/Некорректный ответ статистики/);
 // Network errors must be shown and retriable without a POST.
 mode='network';
 await act(async()=>period().props.onChange({target:{value:'30'}}));await flush();await flush();
 assert.match(JSON.stringify(root.toJSON()),/Синтетический сбой сети/);
 mode='ok';
 await act(async()=>root.root.findAllByType('button').find(b=>b.props.children==='Обновить').props.onClick());await flush();await flush();
 assert.match(JSON.stringify(root.toJSON()),/Частичный ориентир на вакансию/);
 // A delayed response for the previous filter must not replace the current view.
 delayedSource='hh';
 await act(async()=>source().props.onChange({target:{value:'hh'}}));await flush();
 assert.equal(pending.length,1);
 delayedSource='';
 await act(async()=>source().props.onChange({target:{value:'companySite'}}));await flush();await flush();
 assert.equal(source().props.value,'companySite');
 assert.match(JSON.stringify(root.toJSON()),/Частичный ориентир на вакансию/);
 await act(async()=>pending[0].resolve(Response.json(pending[0].result)));await flush();
 assert.equal(source().props.value,'companySite');
 assert.match(JSON.stringify(root.toJSON()),/Частичный ориентир на вакансию/);
 console.log('GO-433 UI: verified payload contract, tiny USD, malformed/invalid responses, retry after network failure, out-of-order filters, GET-only, and partial-cost disclosures passed.');
}finally{
 if(root)await act(async()=>root.unmount());
 Object.assign(globalThis,previous);
}
