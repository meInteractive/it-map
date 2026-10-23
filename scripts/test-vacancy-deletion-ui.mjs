import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const old=fetch;
let root,mode='error',allowed=false,calls=[],confirmations=[],alerts=[],release;
let vacancy={id:73,title:'Delete test',companyName:'Employer',entityType:'company',realCompanyName:'',level:'Middle',telegramSources:[],descriptionMd:'Full text',coverLetterMd:'Saved',archived:false};
const entities=[{name:'Employer',entityType:'company',rank:1,middle:[200000,300000]}];
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const button=text=>root.root.findAllByType('button').find(n=>label(n)===text);
const panel=()=>root.root.findAllByType('aside').find(n=>n.props['aria-label']==='Delete test');
const remove=()=>panel().findAllByType('button').find(n=>n.props.title==='Удалить вакансию');
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0))});
const click=async node=>{await act(async()=>node.props.onClick());await flush()};
try {
 globalThis.window={addEventListener(){},removeEventListener(){},matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};
 globalThis.document={activeElement:null,addEventListener(){},removeEventListener(){}};globalThis.localStorage={getItem:()=>null,setItem(){}};globalThis.location={pathname:'/'};
 globalThis.confirm=message=>{confirmations.push(message);return allowed};globalThis.alert=message=>alerts.push(message);
 globalThis.fetch=async(url,options={})=>{
  calls.push([url,options.method||'GET']);
  if(options.method==='DELETE'){
   await new Promise(r=>release=r);
   if(mode==='error')return Response.json({error:'Delete rejected'},{status:500});
   if(mode==='archive'){vacancy={...vacancy,archived:true};return Response.json({ok:true,archived:true,companyName:'Employer'})}
   vacancy=null;return Response.json({ok:true,archived:false,companyName:'Employer',preservedApplication:true,preservedTask:true});
  }
  if(url==='/api/session')return Response.json({name:'Admin',isAdmin:true});
  if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});
  if(url==='/api/google-calendar/status')return Response.json({configured:false});
  if(url.startsWith('/api/vacancies/list'))return Response.json({vacancies:vacancy?[vacancy]:[],entities,applicationSummary:[]});
  if(url==='/api/vacancies/73')return vacancy?Response.json({vacancy,entities}):Response.json({error:'Вакансия не найдена.'},{status:404});
  throw Error('Unexpected API '+url);
 };
 await act(async()=>{root=create(h(ui.Application))});await flush();await click(button('Вакансии'));await click(button('Delete test'));
 await click(remove());assert.equal(calls.filter(c=>c[1]==='DELETE').length,0);assert.ok(panel(),'cancel keeps drawer');assert.match(confirmations.at(-1),/помечена как архивная/);
 allowed=true;await click(remove());assert.equal(remove().props.disabled,true);await act(async()=>release());await flush();assert.ok(panel());assert.match(label(panel()),/Delete rejected/);assert.equal(remove().props.disabled,false);assert.deepEqual(alerts,[]);
 mode='archive';await click(remove());await act(async()=>release());await flush();assert.ok(panel(),'archive keeps drawer');assert.match(label(panel()),/Архив/);assert.match(alerts.at(-1),/помечена как архивная/);assert.doesNotMatch(label(panel()),/Delete rejected/);
 mode='delete';await click(remove());assert.match(confirmations.at(-1),/окончательно/);assert.match(confirmations.at(-1),/отклики и задачи сохранятся/);await act(async()=>release());await flush();assert.equal(panel(),undefined);assert.match(label(root.root),/В выбранных фильтрах вакансий нет/);assert.match(alerts.at(-1),/сохранены без привязки/);assert.doesNotMatch(label(root.root),/Вакансия не найдена/);
 assert.equal(calls.filter(c=>c[1]==='DELETE').length,3);
 assert.equal(calls.filter(c=>c[0].startsWith('/api/vacancies/list')).length,3,'only successful mutations refresh list');
 console.log('Vacancy deletion UI: cancel, pending, failure/retry, archive kept and refreshed, permanent removal closes drawer and preserves related-data notice.');
} finally {if(root)await act(async()=>root.unmount());ui.clearCache();globalThis.fetch=old;}
