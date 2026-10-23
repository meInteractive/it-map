import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const old=fetch;
const entities=[{name:'Employer',entityType:'company',rank:1,middle:[200000,300000]},{name:'Agency',entityType:'agency',rank:1,middle:[220000,330000]}];
let vacancy={id:61,revision:'a'.repeat(64),title:'Context vacancy',companyName:'Agency',entityType:'agency',realCompanyName:'Employer',level:'Middle',role:'Менеджер проектов',salaryFrom:null,salaryTo:null,descriptionMd:'Full',coverLetterMd:'Saved',telegramSources:[{channelId:7,channelName:'Channel'}]};
let root,calls=[];
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const buttons=()=>root.root.findAllByType('button');
const click=async button=>{assert.ok(button);await act(async()=>button.props.onClick());await act(async()=>{await new Promise(r=>setTimeout(r,0))});};
const byText=text=>buttons().find(b=>label(b)===text);
const mount=async(C,props)=>{await act(async()=>{root=create(h(C,props))});await act(async()=>{await new Promise(r=>setTimeout(r,0))});};
const unmount=async()=>{if(root)await act(async()=>root.unmount());root=null;ui.clearCache()};
const drawer=()=>root.root.findAllByType('aside').find(n=>n.props['aria-label']==='Context vacancy');
try {
 globalThis.window={addEventListener(){},removeEventListener(){},matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};
 globalThis.document={activeElement:null,addEventListener(){},removeEventListener(){}};globalThis.localStorage={getItem:()=>null,setItem(){}};globalThis.location={pathname:'/'};globalThis.confirm=()=>true;
 globalThis.fetch=async (url,options={})=>{calls.push(url);if(options.method==='PUT')return Response.json({vacancies:[vacancy]});if(url==='/api/session')return Response.json({name:'Test',isAdmin:true});if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});if(url==='/api/google-calendar/status')return Response.json({configured:false});if(url==='/api/channels')return Response.json({channels:[{id:7,name:'Channel',url:'https://t.me/test'}]});if(url==='/api/vacancies/61')return Response.json({vacancy,entities});if(url.startsWith('/api/vacancies'))return Response.json({vacancies:[vacancy],entities,applicationSummary:[]});throw Error('Unexpected API '+url)};
 await mount(ui.Application);await click(byText('Вакансии'));
 const chooseView=async title=>{await act(async()=>root.root.findByProps({'data-section-menu':'vacancies'}).props.onPointerEnter({pointerType:'mouse'}));await click(root.root.findByProps({id:'vacancies-navigation'}).findAllByType('button').find(b=>label(b)===title));};
 for(const [tab,name,salary] of [['Агентства','Agency','~ 220к - 330к'],['Компании','Employer','~ 200к - 300к'],['Каналы','Channel','зарплата не указана']]){
  await chooseView(tab);await click(byText('Context vacancy'));
  const panel=drawer();assert.ok(panel);assert.match(label(panel),new RegExp(name));assert.ok(label(panel).includes(salary));
  assert.equal(label(panel.findByProps({className:'vacancy-drawer-company'})),name);
  await click(panel.findByProps({'aria-label':'Закрыть'}));
 }
 assert.equal(calls.filter(p=>p==='/api/vacancies/61').length,1,'detail cache is shared between views');
 await click(byText('Context vacancy'));await click(drawer().findByProps({className:'vacancy-drawer-company'}).findByType('button'));
 assert.ok(calls.includes('/api/vacancies?channel=7'),'channel subtitle opens channel instead of company');
 await click(byText('Context vacancy'));assert.equal(label(drawer().findByProps({className:'vacancy-drawer-company'})),'Channel');
 await click(drawer().findByProps({'aria-label':'Закрыть'}));
 await chooseView('Компании');await click(byText('Context vacancy'));await click(byText('Редактировать'));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 assert.equal(label(drawer().findByProps({className:'vacancy-drawer-company'})),'Employer','editing an agency-owned vacancy preserves employer view');
 assert.ok(label(drawer()).includes('~ 200к - 300к'));
 await unmount();
 for(const [type,name,salary] of [['agency','Agency','~ 220к - 330к'],['company','Employer','~ 200к - 300к']]){
  const opened=[];await mount(ui.CompanyJobs,{companyName:name,type,nav:{...nav,openVacancy:(...args)=>opened.push(args)},admin:false,onCreate(){}});
  assert.ok(label(root.root).includes(salary));await click(byText('Context vacancy'));await click(byText('Полный текст'));assert.deepEqual(opened,[[61,type],[61,type]]);await unmount();
 }
 for(const view of ['company','agency','channel']){
  vacancy={...vacancy,salaryFrom:400000,salaryTo:500000};
  await mount(ui.VacancyDrawer,{id:61,view,nav,admin:false,onClose(){},onEdit(){}});
  assert.ok(label(drawer()).includes('400 000 — 500 000 ₽'),'explicit salary overrides context estimate');await unmount();
 }
 const opened=[];await mount(ui.VacancyDrawer,{id:61,nav:{...nav,openCompany:(...args)=>opened.push(args)},admin:false,onClose(){},onEdit(){}});
 await click(drawer().findByProps({className:'vacancy-drawer-company'}).findByType('button'));assert.deepEqual(opened,[['Employer','company']]);await unmount();
 assert.deepEqual(ui.vacancyContext({...vacancy,realCompanyName:''},'company'),{name:'Agency',type:'agency',channelId:undefined});

 // Vacancy-scoped forms retain the owning agency even when opened in employer view.
 // Exercise existing and new drafts, delayed save/error/retry and native locking.
 for(const existing of [false,true]){
  let application=existing?{id:19,revision:'b'.repeat(64),companyName:'Agency',vacancyId:61,vacancyTitle:vacancy.title,status:'Откликнулся',comment:'Original',applicationSources:[],history:[{fromStatus:'',toStatus:'Откликнулся',changedAt:'2026-10-06 10:00:00'}]}:null;
  const unrelated={id:20,companyName:'Agency',vacancyId:null,vacancyTitle:'',status:'Офер',comment:'Unrelated',history:[]};
  let sent,release,pending,writes=0,fail=true;
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/vacancies/61')return Response.json({vacancy,entities});
   if(url==='/api/applications?company=Agency')return Response.json({records:[unrelated,...(application?[application]:[])],vacancies:[vacancy]});
   if(url==='/api/applications/create-context')return Response.json({creationContext:'ac1.'+'A'.repeat(43)});
   assert.equal(url,existing?'/api/applications/19':'/api/applications/create');
   writes++;sent={method:options.method,body:JSON.parse(options.body)};
   await new Promise(resolve=>release=resolve);
   if(fail)return Response.json({error:'Synthetic vacancy application failure'},{status:503});
   application={...sent.body,id:19,revision:'c'.repeat(64),vacancyTitle:vacancy.title,history:[]};
   return Response.json({records:[application]});
  };
  await mount(ui.VacancyDrawer,{id:61,view:'company',nav,admin:false,onClose(){},onEdit(){}});
  await click(byText('Отклик'));
  assert.equal(root.root.findAllByType('form').length,1);
  assert.equal(root.root.findByType('textarea').props.value,existing?'Original':'');
  assert.ok(!label(drawer()).includes('Unrelated'));
  assert.ok(label(drawer()).includes('История изменения отклика'));
  assert.ok(label(drawer()).includes(existing?'06.10.2026, 13:00 · Нет отклика → Откликнулся':'История пока пустая'));
  await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Scoped draft'}}));
  const submit=root.root.findByType('form').props.onSubmit;
  await act(async()=>{pending=submit({preventDefault(){}});void submit({preventDefault(){}});});
  assert.equal(writes,1);assert.equal(sent.method,existing?'PUT':'POST');
  assert.equal(sent.body.companyName,'Agency');assert.equal(sent.body.vacancyId,61);
  if(existing)assert.equal(sent.body.revision,'b'.repeat(64));else assert.ok(sent.body.creationContext);
  for(const control of root.root.findByType('form').findAll(n=>['input','select','textarea'].includes(n.type))){
   let locked=false;for(let parent=control;parent;parent=parent.parent)if(typeof parent.type==='string'&&parent.props.disabled)locked=true;
   assert.equal(locked,true,'Vacancy application fields stay locked during saving');
  }
  await act(async()=>{release();await pending;});
  assert.match(label(drawer()),/Synthetic vacancy application failure/);
  assert.equal(root.root.findByType('textarea').props.value,'Scoped draft');
  fail=false;await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});
  await act(async()=>{release();await pending;});assert.equal(writes,2);assert.equal(sent.body.comment,'Scoped draft');
  if(existing){
   await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Next own edit'}}));
   await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});
   await act(async()=>{release();await pending;});
   assert.equal(sent.body.revision,'c'.repeat(64),'Own successful PUT must advance the frozen base before another save');
   assert.equal(sent.body.comment,'Next own edit');
  }
  await unmount();
 }

 // Read recovery must work both before opening a form and after an acknowledged
 // POST whose follow-up list GET fails. Retrying reads must never create again.
 let readFailure=true,created=null,writes=0;
 globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications?company=Agency')return readFailure?Response.json({error:'Synthetic application read failure'},{status:503}):Response.json({records:created?[created]:[],vacancies:[vacancy]});
  if(url==='/api/applications/create-context')return Response.json({creationContext:'ac1.'+'A'.repeat(43)});
  assert.equal(url,'/api/applications/create');assert.equal(options.method,'POST');writes++;
  created={...JSON.parse(options.body),id:31,revision:'d'.repeat(64),vacancyTitle:vacancy.title,history:[]};
  readFailure=true;
  return Response.json({records:[created],createdApplicationId:31,replayed:false});
 };
 await mount(ui.CompanyApplications,{companyName:'Agency',vacancyId:61});
 assert.match(label(root.toJSON()),/Synthetic application read failure/);
 assert.ok(byText('Повторить загрузку откликов'),'Initial read error offers retry');
 readFailure=false;await click(byText('Повторить загрузку откликов'));assert.equal(root.root.findAllByType('form').length,1);
 await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Saved despite read failure'}}));
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 assert.equal(writes,1);assert.match(label(root.toJSON()),/Synthetic application read failure/);
 assert.ok(byText('Повторить загрузку откликов'),'Follow-up read error offers retry');
 readFailure=false;await click(byText('Повторить загрузку откликов'));
 assert.equal(writes,1,'Retry must only GET, never repeat an acknowledged POST');
 assert.equal(root.root.findByType('textarea').props.value,'Saved despite read failure');await unmount();

 // Successful deletion returns the vacancy tab to a fresh draft. Failed delete
 // preserves input; company mode keeps its ordinary closed-form behavior.
 for(const scoped of [true,false]){
  let stored={id:41,revision:'e'.repeat(64),companyName:'Agency',vacancyId:61,vacancyTitle:vacancy.title,status:'Откликнулся',comment:'Before deletion',applicationSources:[],history:[]};
  let failDelete=true,deletes=0,contexts=0,posts=0;
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/applications?company=Agency')return Response.json({records:stored?[stored]:[],vacancies:[vacancy]});
   if(url==='/api/applications/create-context'){contexts++;return Response.json({creationContext:'ac1.'+'B'.repeat(43)});}
   if(url==='/api/applications/create'){
    posts++;const body=JSON.parse(options.body);assert.equal(body.vacancyId,61);assert.equal(body.companyName,'Agency');
    assert.equal(body.comment,'');assert.equal(body.creationContext,'ac1.'+'B'.repeat(43));assert.match(body.idempotencyKey,/^[a-f0-9-]{36}$/);
    stored={...body,id:42,revision:'f'.repeat(64),vacancyTitle:vacancy.title,history:[]};return Response.json({records:[stored],createdApplicationId:42,replayed:false});
   }
   assert.equal(url,'/api/applications/41');assert.equal(options.method,'DELETE');deletes++;
   if(failDelete)return Response.json({error:'Synthetic delete failure'},{status:503});
   stored=null;return Response.json({records:[]});
  };
  await mount(ui.CompanyApplications,{companyName:'Agency',...(scoped?{vacancyId:61}:{})});
  if(!scoped)await click(buttons().find(b=>String(b.props.className).startsWith('company-application-card')));
  await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Retained on delete failure'}}));
  const remove=()=>buttons().find(b=>b.props['aria-label']==='Удалить отклик '+vacancy.title);
  await click(remove());assert.match(label(root.toJSON()),/Synthetic delete failure/);assert.equal(root.root.findByType('textarea').props.value,'Retained on delete failure');
  assert.equal(contexts,0);failDelete=false;await click(remove());assert.equal(deletes,2);
  assert.equal(root.root.findAllByType('form').length,scoped?1:0,'Vacancy reopens a new form; company closes its old one');
  assert.equal(posts,0,'Deletion itself must not create');
  if(scoped){assert.equal(root.root.findByType('textarea').props.value,'');assert.equal(contexts,1);await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.equal(posts,1);}
  else assert.equal(contexts,0);
  await unmount();
 }
 // Deletion must lock the parent editor and selectors while the request is held.
 // Two invocations from the same render must send one DELETE and one confirm.
 for(const scoped of [false,true]){
  const first={id:51,revision:'a'.repeat(64),companyName:'Agency',vacancyId:61,vacancyTitle:'First application',status:'Откликнулся',comment:'First draft',applicationSources:[],history:[]};
  const second={...first,id:52,vacancyTitle:'Second application',comment:'Second draft'};
  let stored=[first,second],deletes=0,writes=0,confirms=0,failDelete=true,release,pending;
  globalThis.confirm=()=>{confirms++;return true;};
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/applications?company=Agency')return Response.json({records:stored,vacancies:[vacancy]});
   if(url==='/api/applications/create-context')return Response.json({creationContext:'ac1.'+'C'.repeat(43)});
   writes++;assert.equal(url,'/api/applications/51');assert.equal(options.method,'DELETE');deletes++;
   await new Promise(resolve=>release=resolve);
   if(failDelete)return Response.json({error:'Synthetic held delete failure'},{status:503});
   stored=[second];return Response.json({records:stored});
  };
  await mount(ui.CompanyApplications,{companyName:'Agency',...(scoped?{vacancyId:61}:{})});
  if(!scoped)await click(buttons().find(b=>label(b).includes('First application')&&String(b.props.className).startsWith('company-application-card')));
  await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Retain held delete draft'}}));
  const remove=()=>buttons().find(b=>b.props['aria-label']==='Удалить отклик First application');
  const handler=remove().props.onClick;
  await act(async()=>{pending=handler();void handler();});
  console.log(JSON.stringify({deleteProbe:{scoped,deletes,confirms,formBusy:root.root.findByType('form').props['aria-busy'],newDisabled:byText('Новый отклик').props.disabled}}));
  assert.equal(deletes,1,'Same-render clicks must not send duplicate DELETE');assert.equal(confirms,1);
  assert.equal(root.root.findByType('form').props['aria-busy'],true,'Deleting locks the current editor');
  for(const control of root.root.findByType('form').findAll(n=>['input','select','textarea'].includes(n.type))){
   let locked=false;for(let p=control;p;p=p.parent)if(typeof p.type==='string'&&p.props.disabled)locked=true;
   assert.equal(locked,true,'Every editor input stays locked during DELETE');
  }
  assert.equal(byText('Новый отклик').props.disabled,true);
  for(const b of buttons().filter(b=>b.props.title==='Удалить отклик'||String(b.props.className).startsWith('company-application-card')))assert.equal(b.props.disabled,true);
  await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.equal(writes,1,'Disabled submit sends no PUT/POST alongside DELETE');
  await act(async()=>{release();await pending;});assert.match(label(root.toJSON()),/Synthetic held delete failure/);
  assert.equal(root.root.findByType('form').props['aria-busy'],false);assert.equal(byText('Новый отклик').props.disabled,false);
  assert.equal(root.root.findByType('textarea').props.value,'Retain held delete draft');
  failDelete=false;await act(async()=>{pending=remove().props.onClick();});
  await act(async()=>{release();await pending;});await act(async()=>{await new Promise(r=>setTimeout(r,0))});
  assert.equal(deletes,2);assert.equal(byText('Новый отклик').props.disabled,false);
  assert.equal(root.root.findAllByType('form').length,scoped?1:0);
  if(scoped)assert.equal(root.root.findByType('textarea').props.value,'Second draft');
  await unmount();
 }
 globalThis.confirm=()=>true;
 // Removing a different record must preserve the selected form even if the
 // mutation's follow-up GET fails. A new intent also survives another deletion.
 for(const mode of ['company-existing','company-new','vacancy-existing']){
  const scoped=mode==='vacancy-existing',creating=mode==='company-new';
  const first={id:71,revision:'a'.repeat(64),companyName:'Agency',vacancyId:61,vacancyTitle:'First sibling',status:'Откликнулся',comment:'First saved',applicationSources:[],history:[]};
  const second={...first,id:72,vacancyTitle:'Second sibling',comment:'Second saved'};
  const selected=scoped?second:first,removed=scoped?first:second;
  let stored=[first,second],readFailure=false,deletes=0,contexts=0,bodies=[];
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/applications?company=Agency')return readFailure?Response.json({error:'Synthetic sibling read failure'},{status:503}):Response.json({records:stored,vacancies:[vacancy]});
   if(url==='/api/applications/create-context'){contexts++;return Response.json({creationContext:'ac1.'+'D'.repeat(43)});}
   if(options.method==='DELETE'){
    assert.equal(url,'/api/applications/'+removed.id);deletes++;stored=stored.filter(r=>r.id!==removed.id);
    readFailure=true;return Response.json({records:stored});
   }
   assert.equal(options.method,creating?'POST':'PUT');assert.equal(url,creating?'/api/applications/create':'/api/applications/'+selected.id);
   const body=JSON.parse(options.body);bodies.push(body);assert.equal(body.companyName,'Agency');
   if(creating&&bodies.length===1)return Response.json({error:'Synthetic initial create failure'},{status:503});
   if(!creating){assert.equal(body.revision,selected.revision);assert.equal(body.vacancyId,61);assert.ok(!Object.hasOwn(body,'idempotencyKey'));}
   const saved={...body,id:creating?73:selected.id,revision:'b'.repeat(64),history:[]};
   stored=creating?[...stored,saved]:stored.map(r=>r.id===selected.id?saved:r);
   return Response.json({records:stored,...(creating?{createdApplicationId:73,replayed:false}:{})});
  };
  await mount(ui.CompanyApplications,{companyName:'Agency',...(scoped?{vacancyId:61}:{})});
  if(creating)await click(byText('Новый отклик'));
  else await click(buttons().find(b=>String(b.props.className).startsWith('company-application-card')&&label(b).includes(selected.vacancyTitle)));
  await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Preserved sibling draft'}}));
  if(creating){await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.match(label(root.toJSON()),/Synthetic initial create failure/);}
  assert.equal(contexts,creating?1:0);
  await click(buttons().find(b=>b.props['aria-label']==='Удалить отклик '+removed.vacancyTitle));
  assert.equal(deletes,1);assert.match(label(root.toJSON()),/Synthetic sibling read failure/);
  console.log(JSON.stringify({siblingProbe:{mode,forms:root.root.findAllByType('form').length,contexts}}));
  assert.equal(root.root.findAllByType('form').length,1,'Deleting another record must keep the selected draft mounted');
  assert.equal(root.root.findByType('textarea').props.value,'Preserved sibling draft');
  assert.equal(root.root.findByType('form').props['aria-busy'],false);
  assert.equal(bodies.length,creating?1:0,'Deleting a sibling never saves or creates the active draft');
  readFailure=false;await click(byText('Повторить загрузку откликов'));
  assert.equal(deletes,1);assert.equal(contexts,creating?1:0,'New intent must not be remounted by another deletion or GET retry');
  assert.equal(root.root.findByType('textarea').props.value,'Preserved sibling draft');
  await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
  assert.equal(bodies.at(-1).comment,'Preserved sibling draft');
  if(creating){assert.equal(bodies.length,2);assert.deepEqual(bodies[1],bodies[0],'Explicit retry keeps original UUID/context/target/payload');}
  else assert.equal(bodies.length,1);
  assert.equal(root.root.findAllByType('form').length,scoped?1:0);
  await unmount();
 }
 // A confirmed selected deletion must not reappear from cached data when GET fails.
 for(const scoped of [true,false])for(const remaining of [false,true]){
  const first={id:81,revision:'a'.repeat(64),companyName:'Agency',vacancyId:61,vacancyTitle:'Deleted selected',status:'Откликнулся',comment:'Deleted saved',applicationSources:[],history:[]};
  const second={...first,id:82,vacancyTitle:'Remaining selected',comment:'Remaining saved'};
  let stored=remaining?[first,second]:[first],readFailure=false,deletes=0,contexts=0,posts=0;
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/applications?company=Agency')return readFailure?Response.json({error:'Synthetic selected read failure'},{status:503}):Response.json({records:stored,vacancies:[vacancy]});
   if(url==='/api/applications/create-context'){contexts++;return Response.json({creationContext:'ac1.'+'E'.repeat(43)});}
   if(options.method==='DELETE'){assert.equal(url,'/api/applications/81');deletes++;stored=stored.filter(r=>r.id!==81);readFailure=true;return Response.json({records:stored});}
   assert.equal(url,'/api/applications/create');assert.equal(options.method,'POST');posts++;
   const body=JSON.parse(options.body);assert.equal(body.comment,'Fresh after confirmed delete');assert.equal(body.vacancyId,61);
   stored=[{...body,id:83,revision:'b'.repeat(64),history:[]}];return Response.json({records:stored,createdApplicationId:83});
  };
  await mount(ui.CompanyApplications,{companyName:'Agency',...(scoped?{vacancyId:61}:{})});
  if(!scoped)await click(buttons().find(b=>String(b.props.className).startsWith('company-application-card')&&label(b).includes(first.vacancyTitle)));
  await click(buttons().find(b=>b.props['aria-label']==='Удалить отклик '+first.vacancyTitle));
  assert.equal(deletes,1);assert.match(label(root.toJSON()),/Synthetic selected read failure/);
  console.log(JSON.stringify({selectedDeleteProbe:{scoped,remaining,comments:root.root.findAllByType('textarea').map(t=>t.props.value)}}));
  assert.equal(buttons().some(b=>b.props['aria-label']==='Удалить отклик '+first.vacancyTitle),false,'Acknowledged deleted record must disappear even on failed GET');
  assert.equal(root.root.findAllByType('form').length,scoped?1:0);
  if(scoped)assert.equal(root.root.findByType('textarea').props.value,remaining?'Remaining saved':'');
  assert.equal(contexts,scoped&&!remaining?1:0);assert.equal(posts,0);
  if(scoped&&!remaining)await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:'Fresh after confirmed delete'}}));
  // Remount uses the same cache: a DELETE ACK must survive panel close/reopen.
  if(!scoped){await act(async()=>root.unmount());root=null;await mount(ui.CompanyApplications,{companyName:'Agency'});assert.equal(buttons().some(b=>b.props['aria-label']==='Удалить отклик '+first.vacancyTitle),false);}
  readFailure=false;await click(byText('Повторить загрузку откликов'));
  assert.equal(deletes,1);assert.equal(contexts,scoped&&!remaining?1:0);
  if(scoped&&!remaining){assert.equal(root.root.findByType('textarea').props.value,'Fresh after confirmed delete');await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.equal(posts,1);}
  await unmount();
 }
 console.log('Vacancy context: views/cache/salary, own ACK revision, read-error retry with one POST, failed deletion draft retention, fresh vacancy creation/company close, held DELETE locks/same-render deduplication, sibling deletion keeps selected/new drafts through GET retry and identical POST retry passed.');
} finally {await unmount();globalThis.fetch=old;}
