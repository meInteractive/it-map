import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';
const {default:ParserFunnel,FunnelView}=await moduleFor('frontend/features/settings/ParserFunnel.tsx'),old=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
const counts={received:4,accepted:3,fulltext:4,structured:2,roles:2,published:2};
const report={checkedAt:Date.now(),timezone:'Europe/Moscow',from:'2026-09-23',to:'2026-10-22',days:30,filters:{sourceId:'all',parser:'all'},sourceOptions:[{id:'hh',name:'HH'},{id:'habr',name:'Хабр'}],parserOptions:[{id:'firecrawl',name:'Firecrawl'},{id:'apify',name:'Apify'},{id:'unknown',name:'Не определён'}],daily:Array.from({length:30},(_,i)=>({day:new Date(Date.UTC(2026,8,23+i)).toISOString().slice(0,10),...Object.fromEntries(Object.keys(counts).map(k=>[k,i===29?counts[k]:0]))})),totals:counts,averages:Object.fromEntries(Object.entries(counts).map(([k,v])=>[k,v/30])),cohort:{...counts,rejected:1,error:1,waiting:0,created:1,linked:1,undatedPublished:0}};
let html=render(FunnelView,{report});assert.match(html,/Динамика по датам этапов/);assert.match(html,/Воронка поступивших за 30 дней/);assert.match(html,/stroke-dasharray="6 4"/);assert.match(html,/Повторные попытки/);assert.match(html,/рол/);assert.equal((html.match(/<tbody>.*?<\/tbody>/s)[0].match(/<tr>/g)||[]).length,30);assert.ok(!html.includes('NaN'));
const empty={...report,totals:Object.fromEntries(Object.keys(counts).map(k=>[k,0])),averages:Object.fromEntries(Object.keys(counts).map(k=>[k,0])),cohort:Object.fromEntries(Object.keys(report.cohort).map(k=>[k,0])),daily:report.daily.map(p=>({...p,...Object.fromEntries(Object.keys(counts).map(k=>[k,0]))}))};assert.ok(!render(FunnelView,{report:empty}).includes('NaN'));
let root;const requests=[];
globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{assert.ok(!options.method||options.method==='GET');assert.ok(url.startsWith('/api/scheduler/funnel?'));const params=new URL(url,'https://fixture').searchParams,filters={sourceId:params.get('sourceId'),parser:params.get('parser')};requests.push(filters);return Response.json({...report,filters});};
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});
try{
 await act(async()=>{root=create(h(ParserFunnel));});await flush();
 const source=()=>root.root.findByProps({'aria-label':'Источник воронки'}),parser=()=>root.root.findByProps({'aria-label':'Парсер воронки'}),checks=()=>root.root.findAllByProps({type:'checkbox'});
 assert.equal(source().props.value,'all');assert.equal(parser().props.value,'all');assert.ok(source().props.className.includes('settings-select'));assert.ok(parser().props.className.includes('settings-select'));
 await act(async()=>checks()[1].props.onChange());assert.equal(checks()[1].props.checked,true);
 await act(async()=>source().props.onChange({target:{value:'hh'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'hh',parser:'all'});
 await act(async()=>parser().props.onChange({target:{value:'firecrawl'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'hh',parser:'firecrawl'});
 await act(async()=>source().props.onChange({target:{value:'habr'}}));await flush();assert.deepEqual(requests.at(-1),{sourceId:'habr',parser:'firecrawl'});assert.equal(parser().props.value,'firecrawl');assert.equal(checks()[1].props.checked,true,'selected series survives filter reload');
 await act(async()=>parser().props.onChange({target:{value:'all'}}));await flush();assert.equal(source().props.value,'habr');
 await act(async()=>{for(const check of checks())if(check.props.checked)check.props.onChange();});assert.match(JSON.stringify(root.toJSON()),/Выберите хотя бы один этап/);
 console.log('Funnel UI: two default series, dashed averages, 30 accessible daily rows, zero-safe funnel, new vs linked and state labels, independently intersected source/parser filters, persistent selected series, styled selects and GET-only analytics passed.');
}finally{if(root)await act(async()=>root.unmount());Object.assign(globalThis,old);}
