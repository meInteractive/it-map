import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
let root,release,calls=[],saved,fail=true,hold=true;
const old=fetch;
const company={name:'Employer',entityType:'company',rank:1,offices:[],middle:[200000,300000]};
let vacancy={id:91,revision:'a'.repeat(64),title:'Edit test',companyName:'Employer',entityType:'company',telegramSources:[],level:'Middle',role:'Менеджер проектов',salaryFrom:210000,salaryTo:300000,url:'https://example.test/job',descriptionMd:'## Full vacancy\nDetailed text',coverLetterMd:'**Saved letter**',coverLetterSubject:'Subject',publishedAt:'2026-10-01',loadedAt:'2026-10-02'};
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const buttons=()=>root.root.findAllByType('button');
const button=text=>buttons().find(b=>label(b)===text);
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0))});
const click=async b=>{assert.ok(b);await act(async()=>b.props.onClick());await flush()};
const mount=async(C,props)=>{await act(async()=>{root=create(h(C,props))});await flush()};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null;ui.clearCache()};
const detailCalls=()=>calls.filter(c=>c[0]==='/api/vacancies/91'&&c[1]==='GET').length;
try {
 globalThis.window={addEventListener(){},removeEventListener(){},matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};
 globalThis.document={activeElement:null,addEventListener(){},removeEventListener(){}};globalThis.localStorage={getItem:()=>null,setItem(){}};globalThis.location={pathname:'/'};globalThis.confirm=()=>true;
 globalThis.fetch=async(url,options={})=>{
  calls.push([url,options.method||'GET']);
  if(options.method==='PUT'){assert.equal(url,'/api/vacancies/91');saved=JSON.parse(options.body);vacancy={...vacancy,...saved};return Response.json({vacancies:[vacancy]})}
  if(url==='/api/session')return Response.json({name:'Admin',isAdmin:true});
  if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});
  if(url==='/api/google-calendar/status')return Response.json({configured:false});
  if(url==='/api/companies/list')return Response.json({companies:[company,{...company,name:'Unranked',rank:null}]});
  if(url==='/api/companies/detail?name=Employer')return Response.json({company});
  if(url==='/api/channels')return Response.json({channels:[]});
  if(url==='/api/vacancies?company=Employer')return Response.json({vacancies:[{...vacancy,descriptionMd:undefined,coverLetterMd:undefined,coverLetterSubject:undefined}],entities:[company]});
  if(url==='/api/vacancies/91'){if(hold)await new Promise(r=>release=r);return fail?Response.json({error:'Detail failed'},{status:503}):Response.json({vacancy,entities:[company]})}
  throw Error('Unexpected API '+url);
 };
 await mount(ui.Application);await click(button('Список'));
 assert.deepEqual(root.root.findAllByProps({className:'company-rank-cell'}).map(label),['№ 1','']);
 await click(button('Employer'));
 assert.equal(label(root.root.findByProps({className:'rank'})),'№ 1');
 await click(buttons().find(b=>b.props['aria-label']==='Вакансии'));
 assert.equal(detailCalls(),0,'company jobs do not preload full details');
 const edit=button('Редактировать');await act(async()=>{edit.props.onClick();edit.props.onClick()});await flush();assert.equal(detailCalls(),1);assert.equal(button('Загрузка…').props.disabled,true);assert.equal(root.root.findAllByType('form').length,0);
 await act(async()=>release());await flush();assert.match(label(root.root),/Detail failed/);assert.equal(button('Редактировать').props.disabled,false);assert.equal(root.root.findAllByType('form').length,0);
 fail=false;await click(button('Редактировать'));await act(async()=>release());await flush();hold=false;
 assert.ok(root.root.findAllByType('textarea').some(t=>t.props.value===vacancy.descriptionMd));assert.ok(root.root.findAllByType('textarea').some(t=>t.props.value===vacancy.coverLetterMd));assert.ok(root.root.findAllByType('input').some(t=>t.props.value==='210000'));assert.ok(root.root.findAllByType('input').some(t=>t.props.value==='300000'));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));await flush();
 assert.equal(saved.descriptionMd,vacancy.descriptionMd);assert.equal(saved.coverLetterMd,undefined);assert.equal(vacancy.coverLetterMd,'**Saved letter**');assert.equal(saved.salaryFrom,210000);assert.equal(saved.salaryTo,300000);
 const detail=root.root.findAllByType('aside').find(n=>n.props['aria-label']==='Edit test');assert.ok(detail);await click(detail.findByProps({'aria-label':'Закрыть'}));const before=detailCalls();await click(button('Редактировать'));assert.equal(detailCalls(),before,'reuse detail cache after save');assert.ok(root.root.findByType('form'));await stop();
 await mount(ui.CompanyJobs,{companyName:'Employer',admin:false,nav,onCreate(){},onEdit(){throw Error('Not allowed')}});assert.equal(button('Редактировать'),undefined);await stop();
 let opened=0;hold=true;
 await mount(ui.CompanyJobs,{companyName:'Employer',admin:true,nav,onCreate(){},onEdit(){opened++}});await click(button('Редактировать'));await stop();release();await flush();assert.equal(opened,0,'late detail cannot open editor after leaving company');
 console.log('Company job editing: admin-only, lazy full detail, duplicate-click guard, loading/error/retry, Markdown/salary preservation, save/cache and unmount guard passed.');
} finally {await stop();globalThis.fetch=old;}
