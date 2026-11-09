import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';

const {default:ParserUnitCost,UnitCostView}=await moduleFor('frontend/features/settings/ParserUnitCost.tsx');
const previous=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
const days=Array.from({length:30},(_,i)=>new Date(Date.UTC(2026,9,11+i)).toISOString().slice(0,10));
const ledger={confirmedUsd:0.5,confirmedOperations:2,unknownReservedUsd:0.26,unknownOperations:1,
 unpricedOperations:0,conflictingOperations:0,unavailableForSource:false};
const lineage={attempts:6,withJob:5,withCandidate:3,withMainVacancy:3,withMatchingCreate:2,
 missingJob:1,missingCandidate:1,missingMainVacancy:0,sourceMismatch:1,
 knownUsdWithMainLink:0.3,knownUsdWithoutMainLink:0,verifiedProviderAttribution:false};
const report={
 checkedAt:Date.now(),timezone:'Europe/Moscow',from:days[0],to:days.at(-1),periodDays:30,
 sourceId:'all',sourceOptions:[{id:'hh',name:'HH'},{id:'companySite',name:'Сайт компании'}],
 newUniqueVacancies:2,undatedCreates:0,
 apify:ledger,ai:{...ledger,confirmedUsd:0.3,unknownReservedUsd:0.5},
 aiLineage:lineage,
 firecrawlCredits:2.5,unknownCreditRows:1,totalConfirmedUsd:0.8,partialPeriodRatioUsd:0.4,
 calendarProxy:true,incompleteCosts:true,daily:days.map((day,i)=>({
  day,newVacancies:i===28||i===29?1:0,aiUsd:i===29?0.3:0,apifyUsd:i===29?0.5:0,observedCredits:i===29?2.5:0
 }))
};
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
const requests=[];
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
 assert.ok(!options.method||options.method==='GET','unit-cost panel must never POST');
 assert.ok(url.startsWith('/api/scheduler/unit-cost?'));
 const query=new URL(url,'https://ci.invalid').searchParams;
 const days=Number(query.get('days')),source=query.get('sourceId');
 requests.push({days,source});
 return Response.json({...report,periodDays:days,sourceId:source,
  apify:source==='all'?ledger:{...ledger,confirmedUsd:0,unavailableForSource:true},
  daily:report.daily.slice(-days)});
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
 console.log('GO-433 UI: lineage coverage, partial-cost warnings, 30 accessible rows, zero denominator, GET-only source/period filters and no fake provider attribution passed.');
}finally{
 if(root)await act(async()=>root.unmount());
 Object.assign(globalThis,previous);
}
