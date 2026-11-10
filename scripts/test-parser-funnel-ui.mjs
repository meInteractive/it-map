import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';
const {default:ParserFunnel,FunnelView}=await moduleFor('frontend/features/settings/ParserFunnel.tsx'),old=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
const counts={received:4,accepted:3,fulltext:4,structured:2,roles:2,published:2};
const report={checkedAt:Date.now(),timezone:'Europe/Moscow',from:'2026-09-23',to:'2026-10-22',days:30,filters:{sourceId:'all',parser:'all'},sourceOptions:[{id:'hh',name:'HH'},{id:'habr',name:'Хабр'}],parserOptions:[{id:'firecrawl',name:'Firecrawl'},{id:'apify',name:'Apify'},{id:'unknown',name:'Не определён'}],daily:Array.from({length:30},(_,i)=>({day:new Date(Date.UTC(2026,8,23+i)).toISOString().slice(0,10),...Object.fromEntries(Object.keys(counts).map(k=>[k,i===29?counts[k]:0])),newVacancies:i===29?1:0})),totals:{...counts,newVacancies:1},averages:{...Object.fromEntries(Object.entries(counts).map(([k,v])=>[k,v/30])),newVacancies:1/30},undatedNewVacancies:0,cohort:{...counts,rejected:1,error:1,waiting:0,created:1,linked:1,undatedPublished:0}};
let html=render(FunnelView,{report});assert.match(html,/Динамика по датам этапов/);assert.match(html,/Новые уникальные вакансии за 30 дней/);assert.match(html,/Импортировано кандидатов за 30 дней/);assert.match(html,/первый успешный create/);assert.match(html,/Воронка поступивших за 30 дней/);assert.match(html,/stroke-dasharray="6 4"/);assert.match(html,/Повторные попытки/);assert.match(html,/рол/);assert.equal((html.match(/<tbody>.*?<\/tbody>/s)[0].match(/<tr>/g)||[]).length,30);assert.ok(!html.includes('NaN'));
const publicationWindow={asOf:report.to,weekFrom:'2026-10-16',days:report.daily.slice(-7).map((day)=>({
 day:day.day,total:day.newVacancies,inWeek:0,older:day.newVacancies,
 future:0,undated:0,
}))};
const withWindow={...report,publicationWindow};
const windowHtml=render(FunnelView,{report:withWindow});
assert.match(windowHtml,/Почему количество новых карточек отличается от каталога/);
assert.match(windowHtml,/Дата в неделе/);
assert.match(windowHtml,/старше недели/);
assert.match(windowHtml,/разбивка по датам первого импорта за семь дней/i);
assert.match(windowHtml,/Это не число/);
assert.match(windowHtml,/<strong>22.10:<\/strong>/);
const {parserFunnelSnapshot}=await moduleFor('shared/parser-statistics-debug.mjs');
const copied=parserFunnelSnapshot(withWindow);
assert.equal(copied.summary.publicationWindow.days.at(-1).older,1);
assert.equal(copied.summary.publicationWindow.days.at(-1).total,1);
const brokenWindow={...withWindow,publicationWindow:{...publicationWindow,
 days:publicationWindow.days.map((day,i)=>i===6?{...day,older:2}:day)}};
assert.doesNotMatch(render(FunnelView,{report:brokenWindow}),/Почему количество новых карточек отличается/);
assert.doesNotMatch(render(FunnelView,{report}),/Почему количество новых карточек отличается/);
const empty={...report,totals:Object.fromEntries([...Object.keys(counts),'newVacancies'].map(k=>[k,0])),averages:Object.fromEntries([...Object.keys(counts),'newVacancies'].map(k=>[k,0])),cohort:Object.fromEntries(Object.keys(report.cohort).map(k=>[k,0])),daily:report.daily.map(p=>({...p,...Object.fromEntries([...Object.keys(counts),'newVacancies'].map(k=>[k,0]))}))};assert.ok(!render(FunnelView,{report:empty}).includes('NaN'));
let root;const requests=[];
globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{assert.ok(!options.method||options.method==='GET');assert.ok(url.startsWith('/api/scheduler/funnel?'));const params=new URL(url,'https://fixture').searchParams,filters={sourceId:params.get('sourceId'),parser:params.get('parser')};requests.push(filters);return Response.json({...report,filters});};
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});
try{
 await act(async()=>{root=create(h(ParserFunnel));});await flush();
 const source=()=>root.root.findByProps({'aria-label':'Источник воронки'}),parser=()=>root.root.findByProps({'aria-label':'Парсер воронки'}),checks=()=>root.root.findAllByProps({type:'checkbox'});
 assert.equal(source().props.value,'all');assert.equal(parser().props.value,'all');assert.equal(checks().length,7,'new unique series must remain separate from six candidate-stage series');assert.equal(checks()[6].props.checked,true,'unique vacancies must be shown by default');assert.equal(checks()[5].props.checked,false,'imported candidates must not be mistaken for unique vacancies by default');assert.ok(source().props.className.includes('settings-select'));assert.ok(parser().props.className.includes('settings-select'));
 await act(async()=>checks()[1].props.onChange());assert.equal(checks()[1].props.checked,true);
 await act(async()=>source().props.onChange({target:{value:'hh'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'hh',parser:'all'});
 await act(async()=>parser().props.onChange({target:{value:'firecrawl'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'hh',parser:'firecrawl'});
 await act(async()=>source().props.onChange({target:{value:'habr'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'habr',parser:'firecrawl'});assert.equal(parser().props.value,'firecrawl');assert.equal(checks()[1].props.checked,true,'selected series survives filter reload');
 await act(async()=>parser().props.onChange({target:{value:'all'}}));await flush();assert.equal(source().props.value,'habr');
 await act(async()=>{for(const check of checks())if(check.props.checked)check.props.onChange();});assert.match(JSON.stringify(root.toJSON()),/Выберите хотя бы один этап/);
 console.log('Funnel UI: two default series, dashed averages, 30 accessible daily rows, zero-safe funnel, new vs linked and state labels, independently intersected source/parser filters, persistent selected series, styled selects and GET-only analytics passed.');
}finally{if(root)await act(async()=>root.unmount());Object.assign(globalThis,old);}
