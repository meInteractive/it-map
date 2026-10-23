import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
fs.mkdirSync('.tests',{recursive:true});
fs.writeFileSync('.tests/relevance-ui-entry.tsx',"export * from '../frontend/api/client';export * from '../frontend/features/vacancies/Relevance';export {VacancyTable} from '../frontend/features/vacancies/Vacancies';");
const ui=await moduleFor('.tests/relevance-ui-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.window={addEventListener(){},removeEventListener(){},dispatchEvent(){}};
const oldFetch=fetch,nav={openCompany(){},openChannel(){},openVacancy(){}},calls=[];
const vacancy={id:1,title:'One vacancy',companyName:'Fixture',entityType:'company',realCompanyName:'',salaryFrom:null,salaryTo:null,role:'',level:'',relevance:null,applicationStatus:'',publishedAt:null,telegramSources:[]};
let root,release,heldGet,mode='normal',value=80,current=null;
globalThis.fetch=async(url,opts={})=>{
 calls.push({url,method:opts.method||'GET'});
 if(opts.method==='POST'){
  assert.equal(url,'/api/vacancies/1/relevance');assert.equal(opts.body,undefined);
  if(mode==='held')await new Promise(r=>release=r);
  if(mode==='error')return Response.json({error:'Недостаточно средств OpenRouter'},{status:402});
  current=value;return Response.json({vacancyId:1,relevance:value,model:'typesafe/jev-1.13'});
 }
 if(mode==='held-get')return new Promise(r=>heldGet=r);
 return Response.json({vacancies:[{...vacancy,relevance:current},{...vacancy,id:2,title:'Untouched vacancy',relevance:33}],entities:[]});
};
function Probe(){const q=ui.useApi('/api/vacancies/list');return h(ui.VacancyTable,{records:q.data?.vacancies||[],entities:[],nav,admin:false});}
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});
const fields=()=>root.root.findAllByType('button').filter(b=>b.props.className?.includes('vacancy-relevance-button'));
const text=n=>n.children.join('');
async function mount(){await act(async()=>{root=create(h(Probe));});await flush();}
async function click(){await act(async()=>{fields()[0].props.onClick();});await flush();}
try{
 await mount();assert.equal(calls.filter(c=>c.method==='POST').length,0,'Rendering does not start paid inference');assert.equal(text(fields()[0]),'—');
 mode='held';await act(async()=>{fields()[0].props.onClick();fields()[0].props.onClick();});await flush();assert.equal(calls.filter(c=>c.method==='POST').length,1);assert.equal(fields()[0].props.disabled,true);assert.equal(text(fields()[0]),'…');
 await act(async()=>{release();});await flush();assert.equal(text(fields()[0]),'80%');assert.equal(text(fields()[1]),'33%');assert.equal(calls.filter(c=>c.method==='GET').length,1,'Only the clicked score changes; no full list/detail reload');
 mode='error';await click();assert.equal(text(fields()[0]),'80%');assert.equal(fields()[0].props.disabled,false);assert.match(JSON.stringify(root.toJSON()),/Недостаточно средств/);
 mode='normal';value=0;await click();assert.equal(text(fields()[0]),'0%');assert.doesNotMatch(JSON.stringify(root.toJSON()),/Недостаточно средств/);
 value=100;await click();assert.equal(text(fields()[0]),'100%');
 // A GET begun before the POST cannot resurrect the old score after success.
 mode='held-get';let stale;await act(async()=>{ui.invalidate(k=>k==='/api/vacancies/list');stale=ui.load('/api/vacancies/list');});await flush();mode='normal';value=55;await click();assert.equal(text(fields()[0]),'55%');
 await act(async()=>{heldGet(Response.json({vacancies:[{...vacancy,relevance:12}],entities:[]}));await stale;});await flush();assert.equal(text(fields()[0]),'55%');
 // A response from a previous session never updates the new user's cached score.
 mode='held';await act(async()=>fields()[0].props.onClick());await flush();mode='normal';current=42;
 await act(async()=>{ui.clearCache();await ui.load('/api/vacancies/list');});await flush();assert.equal(text(fields()[0]),'42%');
 await act(async()=>{release();});await flush();assert.equal(text(fields()[0]),'42%');
 await act(async()=>root.unmount());root=undefined;ui.clearCache();current=55;await mount();assert.equal(text(fields()[0]),'55%','A reload reads the persisted score');
 console.log('Relevance UI: dash/value clicks, loading, duplicate click suppression, 0/100%, errors keep score, targeted cache update, stale GET/session isolation and reload passed.');
}finally{if(root)await act(async()=>root.unmount());ui.clearCache();globalThis.fetch=oldFetch;}
