import assert from 'node:assert/strict';
import {createElement as h, lazy} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
import {pathToFileURL} from 'node:url';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
function browser(){globalThis.window={addEventListener(){},removeEventListener(){},dispatchEvent(){},matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};globalThis.document={activeElement:null,addEventListener(){},removeEventListener(){}};globalThis.localStorage={getItem:()=>null,setItem(){}};globalThis.location={pathname:'/',replace(){}};globalThis.confirm=()=>true;}
let root;
const flush=async()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});
const mount=async(component,props)=>{browser();await act(async()=>{root=create(h(component,props));});await flush();return root;};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=undefined;ui.clearCache();};
const buttons=()=>root.root.findAllByType('button');
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const button=text=>buttons().find(b=>label(b).includes(text));
const click=async b=>{assert.ok(b);await act(async()=>{await b.props.onClick();});await flush();};
const openEntityMenu=async section=>{await act(async()=>root.root.findByProps({'data-section-menu':section}).props.onPointerEnter({pointerType:'mouse'}));};
const selectEntity=async(section,title)=>{await openEntityMenu(section);const menu=root.root.findByProps({id:section+'-navigation'});await click(menu.findAllByType('button').find(b=>label(b)===title));};
const change=async(node,value)=>act(async()=>node.props.onChange({target:{value}}));
const submit=async()=>{await act(async()=>{await root.root.findByType('form').props.onSubmit({preventDefault(){}});});await flush();};
export async function testCache(){const old=fetch;const calls=[];try{ui.clearCache();globalThis.fetch=async url=>{calls.push(url);return Response.json({url});};assert.equal(calls.length,0);await Promise.all([ui.load('/api/companies/list'),ui.load('/api/companies/list')]);assert.deepEqual(calls,['/api/companies/list']);await ui.load('/api/companies/7');await ui.load('/api/companies/map');await ui.load('/api/companies/list');assert.equal(calls.length,3);ui.invalidateMutation('/api/applications/1');await ui.load('/api/companies/list');assert.equal(calls.length,4);let release;globalThis.fetch=()=>new Promise(r=>release=r);const pending=ui.load('/api/vacancies/list');ui.invalidateMutation('/api/vacancies/1/state');release(Response.json({old:true}));await pending;globalThis.fetch=async()=>Response.json({fresh:true});assert.deepEqual(await ui.load('/api/vacancies/list'),{fresh:true});await testDeletedApplicationCache();}finally{globalThis.fetch=old;ui.clearCache();}}
async function testDeletedApplicationCache(){
 const key='/api/crm/applications',company='/api/applications?company=Synthetic';
 const records=[{id:91,comment:'Deleted'},{id:92,comment:'Keep'}];
 let mode='initial',release,reads=0;
 const Probe=()=>h('output',null,JSON.stringify(ui.useApi(key).data));
 try{
  ui.clearCache();
  globalThis.fetch=async(url,options={})=>{
   if(options.method==='DELETE')return Response.json({records:[records[1]]});
   if(mode==='held'&&url===company)return Response.json({error:'Synthetic company read failure'},{status:503});
   if(mode==='held'&&url===key){reads++;if(reads===1)return new Promise(resolve=>release=resolve);return Response.json({error:'Synthetic cache read failure'},{status:503});}
   return Response.json({records,entities:[]});
  };
  await ui.load(key);await ui.load(company);
  mode='held';ui.invalidateMutation('/api/applications/91');const stale=ui.load(key);
  await ui.mutate('/api/applications/91','DELETE');
  await mount(Probe);assert.doesNotMatch(label(root.toJSON()),/Deleted/);assert.match(label(root.toJSON()),/Keep/);
  await act(async()=>{release(Response.json({records,entities:[]}));await stale;});await flush();
  assert.doesNotMatch(label(root.toJSON()),/Deleted/,'Pre-DELETE GET cannot resurrect the removed record');
  await act(async()=>root.unmount());root=undefined;
  await mount(()=>h('output',null,JSON.stringify(ui.useApi(company).data)));
  assert.doesNotMatch(label(root.toJSON()),/Deleted/,'Company cache is pruned as well');
  // A new successful snapshot can reuse the ID after restore; no permanent tombstone.
  mode='restored';await act(async()=>{ui.clearCache();await ui.load(company);});
  assert.match(label(root.toJSON()),/Deleted/,'Cache reset/restore can load an ID again');
 }finally{await stop();}
}
export async function testInitialLoading(){const old=fetch,calls=[];const base={roles:[],level:null,telegramSources:[],salaryFrom:null,salaryTo:null,favorite:false,hidden:false,loadedAt:'2026-10-21'},vacancyData={entities:[{name:'Employer',entityType:'company',rank:1},{name:'Recruiter',entityType:'agency',rank:1}],applicationSummary:[],vacancies:[{...base,id:901,title:'Company vacancy',companyName:'Employer',entityType:'company'},{...base,id:902,title:'Agency vacancy',companyName:'Recruiter',entityType:'agency'},{...base,id:903,title:'Channel vacancy',companyName:'__channel__',entityType:'channel',telegramSources:[{channelId:1,channelName:'Channel'}]}]};try{globalThis.fetch=async url=>{calls.push(url);return Response.json(url==='/api/session'?{name:'Eugene',isAdmin:true}:url==='/api/tasks'?{tasks:[],entities:[]}:url==='/api/companies/list'?{companies:[]}:url.startsWith('/api/vacancies/list')?vacancyData:url==='/api/crm/applications'?{records:[],entities:[]}:url==='/api/channels'?{channels:[]}:url==='/api/agencies/list'?{companies:[]}:{configured:false});};await mount(ui.Application);assert.equal(calls[0],'/api/session');assert.deepEqual(new Set(calls),new Set(['/api/session','/api/tasks','/api/google-calendar/status']));await click(button('Список'));assert.equal(calls.filter(p=>p==='/api/companies/list').length,1);await click(button('Отклики'));await click(button('Список'));assert.equal(calls.filter(p=>p==='/api/companies/list').length,1);assert.ok(!calls.some(p=>p.startsWith('/api/vacancies')));const search=()=>root.root.findAllByType('input').find(n=>n.props.placeholder?.startsWith('Название'));
await change(search(),'Company query');await click(button('Вакансии'));assert.equal(search().props.value,'');await change(search(),'Vacancy query');assert.equal(search().props.value,'Vacancy query');assert.equal(calls.filter(p=>p==='/api/vacancies/list?period=week').length,1);
await click(button('Список'));assert.equal(search().props.value,'Company query');await click(button('Вакансии'));assert.equal(search().props.value,'Vacancy query');assert.equal(calls.filter(p=>p==='/api/vacancies/list?period=week').length,1);
await change(search(),'');assert.match(label(root.toJSON()),/Company vacancy/);assert.doesNotMatch(label(root.toJSON()),/Agency vacancy|Channel vacancy/);
const beforeHover=calls.length;await openEntityMenu('vacancies');assert.equal(calls.length,beforeHover,'hover does not navigate or fetch');
await selectEntity('vacancies','Агентства');assert.match(label(root.toJSON()),/Вакансии агентств/);assert.match(label(root.toJSON()),/Agency vacancy/);assert.doesNotMatch(label(root.toJSON()),/Company vacancy|Channel vacancy/);assert.equal(root.root.findAllByProps({className:'entity-switch'}).length,0,'no entity filter remains');
await click(button('Вакансии'));assert.match(label(root.toJSON()),/Вакансии компаний/,'main click resets company view');assert.match(label(root.toJSON()),/Company vacancy/);assert.doesNotMatch(label(root.toJSON()),/Agency vacancy|Channel vacancy/);
await selectEntity('vacancies','Каналы');assert.match(label(root.toJSON()),/Вакансии каналов/);assert.match(label(root.toJSON()),/Channel vacancy/);assert.doesNotMatch(label(root.toJSON()),/Company vacancy|Agency vacancy/);await click(button('Вакансии'));assert.match(label(root.toJSON()),/Вакансии компаний/);
await selectEntity('list','Агентства');assert.match(label(root.toJSON()),/Список агентств/);assert.equal(calls.filter(p=>p==='/api/agencies/list').length,1);
await selectEntity('list','Каналы');assert.match(label(root.toJSON()),/Список каналов/);assert.equal(calls.filter(p=>p==='/api/channels').length,1);
await click(button('Список'));assert.match(label(root.toJSON()),/Список компаний/);assert.equal(calls.filter(p=>p==='/api/companies/list').length,1,'main click reuses company cache');
const listNav=()=>root.root.findByProps({'data-section-menu':'list'});await act(async()=>listNav().props.onKeyDown({key:'Escape',preventDefault(){}}));assert.equal(root.root.findByProps({'aria-controls':'list-navigation'}).props['aria-expanded'],false);
const listTrigger=()=>root.root.findByProps({'aria-controls':'list-navigation'});await act(async()=>listTrigger().props.onPointerDown({pointerType:'touch'}));await click(listTrigger());assert.equal(listTrigger().props['aria-expanded'],true,'touch exposes submenu and opens default companies');
await selectEntity('list','Агентства');await click(button('Задачи'));await click(button('Список'));assert.match(label(root.toJSON()),/Список компаний/,'reentry through main item resets previous category');
const beforeSettings=calls.length;
await act(async()=>root.root.findByProps({className:'settings-nav'}).props.onPointerEnter({pointerType:'mouse'}));assert.equal(calls.length,beforeSettings,'Opening navigation does not load settings content');
assert.equal(button('Настройки').props['aria-expanded'],true);
assert.ok(button('Документация'));await click(button('Резюме'));
assert.match(label(root.toJSON()),/Настройки · Резюме/);assert.equal(button('Настройки').props['aria-expanded'],false);
const draft=root.root.findByType('textarea');await change(draft,'Unsaved navigation draft');
await click(button('Настройки'));globalThis.confirm=()=>false;await click(button('Общие'));
assert.equal(root.root.findByType('textarea').props.value,'Unsaved navigation draft');
assert.equal(button('Настройки').props['aria-expanded'],true,'Cancelled navigation keeps menu open');
globalThis.confirm=()=>true;await click(button('Общие'));
assert.match(label(root.toJSON()),/Настройки · Общие/);assert.equal(button('Настройки').props['aria-expanded'],false);
await click(button('Настройки'));await act(async()=>root.root.findByProps({className:'settings-nav'}).props.onKeyDown({key:'Escape',preventDefault(){}}));
assert.equal(button('Настройки').props['aria-expanded'],false);
}finally{await stop();globalThis.fetch=old;}}
export async function testRevisionEditor(){const old=fetch;let sent,fail=false;try{globalThis.fetch=async(url,o)=>{sent=JSON.parse(o.body);return Response.json(fail?{error:'Изменено в другой вкладке'}:{resumeMd:sent.resumeMd,revision:2,updatedAt:'2026-10-03'},{status:fail?409:200});};await mount(ui.RevisionEditor,{initial:{resumeMd:'# Demo',revision:1,updatedAt:null},url:'/api/profile/resume',field:'resumeMd',limit:20000,title:'Резюме',description:''});await change(root.root.findByType('textarea'),'# Updated');await submit();assert.deepEqual(sent,{resumeMd:'# Updated',revision:1});assert.equal(button('Сохранить резюме').props.disabled,true);await click(button('Предпросмотр'));assert.equal(root.root.findAllByType('textarea').length,0);await click(button('Редактировать'));fail=true;await change(root.root.findByType('textarea'),'My draft');await submit();assert.equal(root.root.findByType('textarea').props.value,'My draft');assert.match(JSON.stringify(root.toJSON()),/другой вкладке/);assert.equal(sent.revision,2);await change(root.root.findByType('textarea'),'😀'.repeat(20000));assert.equal(button('Сохранить резюме').props.disabled,false);await change(root.root.findByType('textarea'),'😀'.repeat(20001));assert.equal(button('Сохранить резюме').props.disabled,true);}finally{await stop();globalThis.fetch=old;}}
export async function testTaskSchedule(){const old=fetch;let sent;try{globalThis.fetch=async(url,o)=>{sent=JSON.parse(o.body);return Response.json(sent);};for(const vacancyId of [1,null,undefined,0]){await mount(ui.TaskCard,{task:{id:1,type:'call',companyName:'Synthetic',dueAt:'2026-10-01T21:15:00.000Z',completed:false,vacancyId},nav});await click(button('Изменить дату'));const date=root.root.findAllByType('input').find(n=>n.props.type==='date'),time=root.root.findAllByType('input').find(n=>n.props.type==='time');assert.equal(date.props.value,'2026-10-02');assert.equal(time.props.value,'00:15');assert.ok(date.props.required);await change(date,'2026-10-04');await submit();assert.deepEqual(sent,{dueAt:'2026-10-03T21:15:00.000Z'});assert.equal(root.root.findAllByType('form').length,0);await stop();}}finally{await stop();globalThis.fetch=old;}}
export async function testVacancyEditor(){const old=fetch;let sent,fail=false,saved=0;const role={id:3,stableKey:'it-project-manager',name:'Менеджер проектов',parentId:1,sortOrder:10,usageCount:0,childCount:0,isLeaf:true,path:'IT Manager → Менеджер проектов',descendantIds:[],children:[]};const record={id:42,revision:'a'.repeat(64),companyName:'Synthetic',entityType:'company',title:'Engineer',url:'https://example.test/job',role:'Менеджер проектов',roles:[role],level:'Middle',descriptionMd:'## Full text',salaryFrom:null,salaryTo:300000,relevance:0,telegramSources:[]};try{globalThis.fetch=async(url,o={})=>{if(!o.method)return Response.json(url==='/api/vacancy-roles'?{roles:[role],tree:[role],roots:[role],leaves:[role]}:{channels:[]});sent={url,method:o.method,body:JSON.parse(o.body)};return Response.json(fail?{error:'Synthetic failure'}:{vacancies:[record]},{status:fail?400:200});};await mount(ui.VacancyEditor,{owner:{name:'Synthetic',entityType:'company'},onClose(){},onSaved(){saved++;}});for(const [max,value]of [[300,'Engineer'],[2048,'https://example.test/job']])await change(root.root.findAllByType('input').find(n=>n.props.maxLength===max),value);await act(async()=>root.root.findAllByType('input').find(n=>n.props.type==='checkbox').props.onChange({target:{checked:true}}));await change(root.root.findAllByType('textarea')[0],'## Full text');const nums=root.root.findAllByType('input').filter(n=>n.props.type==='number');await change(nums[1],'300000');await change(nums[2],'0');await submit();assert.equal(sent.method,'POST');assert.equal(sent.body.salaryFrom,null);assert.equal(sent.body.salaryTo,300000);assert.equal(sent.body.relevance,0);assert.deepEqual(sent.body.roleIds,[3]);assert.ok(!Object.hasOwn(sent.body,'telegramChannelId'));assert.equal(saved,1);await stop();fail=true;await mount(ui.VacancyEditor,{owner:{name:'Synthetic',entityType:'company'},record,onClose(){},onSaved(){saved++;}});await submit();assert.equal(sent.method,'PUT');assert.equal(sent.url,'/api/vacancies/42');assert.match(JSON.stringify(root.toJSON()),/Synthetic failure/);assert.equal(root.root.findAllByType('textarea')[0].props.value,record.descriptionMd);assert.equal(saved,1);assert.equal(root.root.findByType('fieldset').props.disabled,false);}finally{await stop();globalThis.fetch=old;}}
export async function testCoverLetter(){const old=fetch;let calls=0,resolve;function Probe({id,existing=''}){return h('div',null,JSON.stringify(ui.useCoverLetter(id,existing)));}try{globalThis.fetch=async()=>{calls++;await new Promise(r=>resolve=r);const bytes=new TextEncoder().encode(JSON.stringify({type:'progress',model:'Тест',elapsedSeconds:180})+'\n'+JSON.stringify({type:'done',letter:{coverLetterSubject:'Тема',coverLetterMd:'Готовое письмо'}})+'\n');return new Response(new ReadableStream({start(c){for(let i=0;i<bytes.length;i+=7)c.enqueue(bytes.slice(i,i+7));c.close();}}),{headers:{'content-type':'application/x-ndjson'}});};await mount(Probe,{id:98001});const first=ui.generateLetter(98001),second=ui.generateLetter(98001);assert.equal(first,second);assert.equal(calls,1);await act(async()=>{resolve();await first;});assert.match(JSON.stringify(root.toJSON()),/Письмо сохранено/);assert.doesNotMatch(JSON.stringify(root.toJSON()),/Готовое письмо/);await stop();await mount(Probe,{id:98002,existing:'Manual'});assert.equal(calls,1);}finally{await stop();globalThis.fetch=old;}}
export async function testDrawerTabs(){const old=fetch,calls=[];try{globalThis.fetch=async url=>{calls.push(url);return Response.json(url==='/api/vacancies/4'?{vacancy:{id:4,title:'Test',companyName:'Synthetic',entityType:'company',descriptionMd:'full',telegramSources:[],coverLetterMd:'Saved'},entities:[]}:url.startsWith('/api/tasks')?{tasks:[],entities:[]}:url==='/api/applications/create-context'?{creationContext:'ac1.'+'A'.repeat(43)}:{records:[],vacancies:[]});};await mount(ui.VacancyDrawer,{id:4,admin:false,nav,onClose(){},onEdit(){}});assert.deepEqual(calls,['/api/vacancies/4']);await click(button('Инфо'));assert.equal(calls.length,1);assert.match(JSON.stringify(root.toJSON()),/Краткая информация/);await click(button('Задачи'));assert.equal(calls.length,2);assert.match(calls[1],/^\/api\/tasks\?/);await click(button('Отклик'));assert.equal(calls.length,4);assert.match(calls[2],/^\/api\/applications\?/);assert.equal(calls[3],'/api/applications/create-context');await click(button('Описание'));assert.equal(calls.length,4);await click(button('Письмо'));assert.equal(calls.length,4);}finally{await stop();globalThis.fetch=old;}}
export async function testReviewDrawer(){
 const old=fetch,calls=[];
 try{
  globalThis.fetch=async url=>{calls.push(url);throw new Error('Review must use the loaded company detail');};
  await mount(ui.ReviewDrawer,{c:{name:'Synthetic',entityType:'company',reviews:{plus:'Positive text',minus:'Negative text'},wlb:'Normal',wlbScore:0,reviewSources:{dreamJob:'https://example.test/reviews'}},onClose(){}});
  const sources=()=>root.root.findAllByType('a').filter(a=>a.props.href==='https://example.test/reviews');
  assert.equal(sources().length,1);assert.match(JSON.stringify(root.toJSON()),/Positive text/);
  await click(button('Минусы'));assert.equal(button('Минусы').props['aria-selected'],true);assert.equal(sources().length,1);assert.match(JSON.stringify(root.toJSON()),/Negative text/);
  await click(button('Факты'));assert.equal(sources().length,1);assert.match(JSON.stringify(root.toJSON()),/0 \/ 100/);assert.match(JSON.stringify(root.toJSON()),/Факты о компании/);
  await click(button('Источники'));assert.equal(sources().length,1);assert.doesNotMatch(JSON.stringify(root.toJSON()),/Negative text|Факты о компании/);
  await click(button('Плюсы'));assert.equal(sources().length,1);assert.deepEqual(calls,[]);
  await stop();
  await mount(ui.ReviewDrawer,{c:{name:'Agency',entityType:'agency',rank:12,score:91.6,reviewsScore:74.75,relevanceScore:9,revenueScore:0,ethics:9.1,vacancyHonesty:8.2,feedback:7.3,vacancyRelevance:6.4,itExpertise:5.5,clientKnowledge:4.6,processOrganization:3.7,candidateSupport:2.8,revenue:{amount:2500000},reviewSources:{dreamJob:'https://example.test/reviews'}},onClose(){}});
  await click(button('Факты'));
  const facts=label(root.toJSON());
  for(const value of ['Факты об агентстве','№ 12','2,5 млн ₽','92 / 100','74,8 / 80','9 / 10','0 / 10','9,1 / 10','8,2 / 10','7,3 / 10','6,4 / 10','5,5 / 10','4,6 / 10','3,7 / 10','2,8 / 10'])assert.ok(facts.includes(value),value);
  assert.equal(sources().length,1);assert.equal(root.root.findAllByType('textarea').length,0);assert.deepEqual(calls,[]);
  await click(button('Источники'));assert.equal(sources().length,1);assert.doesNotMatch(label(root.toJSON()),/Оценки по отзывам/);
  await click(button('Факты'));assert.match(label(root.toJSON()),/74,8 \/ 80/);assert.deepEqual(calls,[]);
 }finally{await stop();globalThis.fetch=old;}
}
export async function testCatalogRows(){
 const old=fetch,calls=[],counts=[],opened=[];
 const companies=Array.from({length:229},(_,i)=>({id:i+1,name:'Компания '+String(i).padStart(3,'0'),entityType:'company',rank:i+1,offices:[],favorite:false,hidden:false,hasVacancies:false,hasApplications:false}));
 const agencies=Array.from({length:31},(_,i)=>({id:i+1,name:'Агентство '+String(i).padStart(3,'0'),entityType:'agency',rank:null,offices:[],favorite:false,hidden:false,hasVacancies:false,hasApplications:false}));
 const props={nav:{...nav,openCompany:(...args)=>{opened.push(args);}},filters:ui.defaultFilters,query:'',onCount:n=>{counts.push(n);}};
 const names=()=>root.root.findAllByType('button').filter(b=>b.props.className==='company-list-name').map(label);
 try{
  globalThis.fetch=async url=>{calls.push(url);assert.ok(['/api/companies/list','/api/agencies/list'].includes(url),'Only the active entity list is fetched');return Response.json({companies:url==='/api/companies/list'?companies:agencies});};
  await mount(ui.CompanyList,props);
  assert.equal(counts.at(-1),229);assert.deepEqual(names(),companies.map(c=>c.name));assert.deepEqual(calls,['/api/companies/list']);
  await click(button('Компания 228'));assert.deepEqual(opened,[['Компания 228','company']]);
  await act(async()=>root.update(h(ui.CompanyList,{...props,type:'agency'})));await flush();assert.equal(counts.at(-1),31);assert.deepEqual(names(),agencies.map(c=>c.name));
  await act(async()=>root.update(h(ui.CompanyList,props)));await flush();assert.equal(counts.at(-1),229);assert.deepEqual(names(),companies.map(c=>c.name));
  await act(async()=>root.update(h(ui.CompanyList,{...props,query:'Компания 228'})));await flush();
  assert.deepEqual(names(),['Компания 228']);assert.equal(counts.at(-1),1);
  await act(async()=>root.update(h(ui.CompanyList,props)));await flush();assert.equal(names().length,229);
  await act(async()=>root.unmount());root=undefined;
  await mount(ui.CompanyList,props);assert.equal(names().length,229,'Remount retains the complete successful list');
  assert.deepEqual(calls,['/api/companies/list','/api/agencies/list'],'Tab changes, search and remount reuse the list cache');
 }finally{await stop();globalThis.fetch=old;}
}
export async function testPersistedFavoriteFilter(){
 const old=fetch,calls=[],storage=new Map([[ui.rankStorageKey,JSON.stringify({version:1,favoriteOnly:true})]]);
 const companies=Array.from({length:229},(_,i)=>({id:i+1,name:'Synthetic company '+i,entityType:'company',rank:i+1,offices:[],favorite:i<32,hidden:i>=225,hasVacancies:false,hasApplications:false}));
 const agencies=Array.from({length:31},(_,i)=>({id:i+1,name:'Synthetic agency '+i,entityType:'agency',rank:null,offices:[],favorite:false,hidden:false,hasVacancies:false,hasApplications:false}));
 const names=()=>root.root.findAllByType('button').filter(b=>b.props.className==='company-list-name');
 const checkbox=text=>root.root.findAllByType('label').find(l=>label(l)===text)?.findByType('input');
 const toggle=async(text,checked)=>{const input=checkbox(text);assert.ok(input);await act(async()=>input.props.onChange({target:{checked}}));await flush();};
 try{
  globalThis.fetch=async url=>{calls.push(url);if(url==='/api/session')return Response.json({name:'Eugene',isAdmin:true});if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});if(url==='/api/google-calendar/status')return Response.json({configured:false});if(url==='/api/companies/list')return Response.json({companies});if(url==='/api/agencies/list')return Response.json({companies:agencies});throw new Error('Unexpected fixture request');};
  browser();globalThis.localStorage={getItem:key=>storage.get(key)||null,setItem:(key,value)=>{storage.set(key,value);}};
  await act(async()=>{root=create(h(ui.Application));});await flush();
  await click(button('Список'));assert.equal(names().length,32);await click(button('Места'));
  assert.equal(checkbox('Избранные').props.checked,true);
  await selectEntity('list','Агентства');assert.equal(names().length,0);
  await toggle('Избранные',false);assert.equal(names().length,31);assert.equal(JSON.parse(storage.get(ui.rankStorageKey)).favoriteOnly,false);
  await selectEntity('list','Компании');assert.equal(names().length,225);
  await toggle('Показать скрытые',true);assert.equal(names().length,229);
  await click(button('Задачи'));await click(button('Список'));assert.equal(names().length,229);
  assert.equal(checkbox('Избранные').props.checked,false);
  assert.equal(calls.filter(url=>url==='/api/companies/list').length,1);assert.equal(calls.filter(url=>url==='/api/agencies/list').length,1);
 }finally{await stop();globalThis.fetch=old;}
}
export async function testFilterMenuFocus(){
 const docEvents=new Map(),windowEvents=new Map(),inside={},outside={};
 const container={contains:target=>target===inside};let changed;
 const bind=events=>({addEventListener:(type,fn)=>{events.set(type,fn);},removeEventListener:(type,fn)=>{if(events.get(type)===fn)events.delete(type);}});
 const menu=()=>root.root.findAllByType('div').find(d=>d.props.className?.startsWith('rank-filter-menu'));
 try{
  browser();Object.assign(document,bind(docEvents));Object.assign(window,bind(windowEvents));
  await act(async()=>{root=create(h(ui.RankFilter,{value:ui.defaultFilters,count:229,onChange:value=>{changed=value;}}),{createNodeMock:node=>node.props.className==='rank-filter'?container:null});});
  assert.equal(button('Места').props['aria-expanded'],false);
   assert.equal(menu().props.hidden,true);
   await click(button('Места'));assert.equal(menu().props.hidden,false);
   assert.equal(button('Места').props['aria-expanded'],true);
  // An internal pointer can blur the toggle before a label activates its checkbox.
  await act(async()=>{docEvents.get('pointerdown')?.({target:inside});root.root.findByProps({className:'rank-filter'}).props.onBlur({currentTarget:container,relatedTarget:null});});
  assert.equal(menu().props.hidden,false,'A null-target blur must not hide an internal label before its click');
  const input=root.root.findAllByType('label').find(l=>label(l)==='Избранные').findByType('input');
  await act(async()=>input.props.onChange({target:{checked:true}}));assert.equal(changed.favoriteOnly,true);assert.equal(menu().props.hidden,false);
  await act(async()=>root.root.findByProps({className:'rank-filter'}).props.onBlur({currentTarget:container,relatedTarget:inside}));
   assert.equal(menu().props.hidden,false,'Фокус внутри меню не закрывает его');
   await act(async()=>docEvents.get('focusin')({target:inside}));assert.equal(menu().props.hidden,false);
   assert.equal(button('Места').props['aria-expanded'],true);
  await act(async()=>docEvents.get('focusin')({target:outside}));assert.equal(menu().props.hidden,true);
   assert.equal(button('Места').props['aria-expanded'],false);
  await click(button('Места'));await act(async()=>docEvents.get('pointerdown')({target:outside}));assert.equal(menu().props.hidden,true);
  await click(button('Места'));await act(async()=>windowEvents.get('blur')());assert.equal(menu().props.hidden,true);
  await click(button('Места'));assert.equal(button('Места').props['aria-expanded'],true);
   await act(async()=>root.root.findByProps({className:'rank-filter'}).props.onKeyDown({key:'Escape'}));
   assert.equal(menu().props.hidden,true);
   assert.equal(button('Места').props['aria-expanded'],false);
  await click(button('Места'));await stop();assert.equal(docEvents.size,0);assert.equal(windowEvents.size,0);
 }finally{await stop();}
}
export async function testPanelLoading(){
 const old=fetch;
 const visibleAsides=()=>root.toJSON().children.filter(n=>n.type==='aside');
 const deferred=()=>{let release;const promise=new Promise(r=>release=r);return {promise,release:()=>release({default:({kind})=>h('aside',{className:kind==='company'?'panel':'vacancy-drawer'+(kind==='channel'?' channel-drawer':kind==='editor'?' vacancy-editor':'')},'Loaded')})};};
 try{
  // Hold actual React lazy imports: company geometry is left throughout code loading.
  const companyLoad=deferred(),childLoad=deferred();
  const Company=lazy(()=>companyLoad.promise),Child=lazy(()=>childLoad.promise);
  let closed=0;
  const close=()=>{closed++;};
  const Fixture=({kind,child=false})=>h('main',null,
   h(ui.PanelBoundary,{kind:'company',title:'Synthetic',onClose:close},h(Company,{kind:'company'})),
   child&&h(ui.PanelBoundary,{kind,title:'Loading child',onClose:close},h(Child,{kind})));
  await mount(Fixture,{kind:'vacancy'});
  let panels=root.root.findAllByType('aside');assert.equal(panels.length,1);assert.equal(panels[0].props.className,'panel');
  assert.equal(panels[0].props['aria-busy'],'true');
  await click(button(''));assert.equal(closed,1,'Loading panel can be closed');
  await act(async()=>{companyLoad.release();await companyLoad.promise;});await flush();
  for(const kind of ['vacancy','editor','channel']){
   await act(async()=>root.update(h(Fixture,{kind,child:true})));await flush();
   const asides=visibleAsides();assert.equal(asides.length,2);
   assert.equal(asides[0].props.className,'panel');assert.equal(label(asides[0]),'Loaded','Opening a lazy child keeps loaded parent visible');
   assert.equal(asides[1].props.className,'vacancy-drawer'+(kind==='channel'?' channel-drawer':kind==='editor'?' vacancy-editor':''));
   assert.match(label(asides[1]),/Загрузка/);
  }
  await act(async()=>{childLoad.release();await childLoad.promise;});await flush();
  assert.equal(visibleAsides().length,2);assert.equal(label(visibleAsides()[0]),'Loaded');
  assert.equal(visibleAsides()[1].props.className,'vacancy-drawer channel-drawer');
  await stop();
  // Detail API wait, success, cache and error also retain the actual company shell.
  for(const type of ['company','agency']){
   let release,calls=0;globalThis.fetch=url=>{if(url.startsWith('/api/crm?'))return Promise.resolve(Response.json({comment:'',status:''}));calls++;return new Promise(r=>release=r);};
   const props={name:'Synthetic',type,nav,admin:false,onClose(){},onCreate(){},onEdit(){}};
   await mount(ui.CompanyDrawer,props);assert.equal(root.root.findByType('aside').props.className,'panel');
   assert.match(label(root.toJSON()),/Загрузка компании/);
   await act(async()=>release(Response.json({company:{name:'Synthetic',entityType:type,offices:[]}})));await flush();
   assert.equal(root.root.findByType('aside').props.className,'panel');assert.doesNotMatch(label(root.toJSON()),/Загрузка компании/);
   await act(async()=>root.unmount());root=undefined;
   await mount(ui.CompanyDrawer,props);assert.equal(calls,1,'Cached card does not fetch again');
   await stop();
   globalThis.fetch=async()=>Response.json({error:'Synthetic detail failure'},{status:503});
   await mount(ui.CompanyDrawer,props);assert.equal(root.root.findByType('aside').props.className,'panel');
   assert.match(label(root.toJSON()),/Synthetic detail failure/);await stop();
  }
 }finally{await stop();globalThis.fetch=old;}
}
export async function testChannelManagement(){
 const old=fetch;let channels=[{id:1,name:'Synthetic channel',url:'https://t.me/synthetic',vacancyCount:0}],failPost=false,failDelete=false,deleteCalls=0,holdDelete,releaseDelete,saved;
 const requests=[];
 let adminMode=false;
 try{
  globalThis.fetch=async(url,options={})=>{
   requests.push({url,method:options.method||'GET'});
   if(options.method==='POST'){saved=JSON.parse(options.body);if(failPost)return Response.json({error:'Synthetic create failure'},{status:400});channels.push({id:2,...saved,vacancyCount:0});return Response.json({channelId:2},{status:201});}
   if(options.method==='DELETE'){deleteCalls++;if(holdDelete)await new Promise(r=>releaseDelete=r);if(failDelete)return Response.json({error:'Канал является единственным источником вакансии'},{status:409});channels=channels.filter(c=>url!=='/api/channels/'+c.id);return Response.json({deleted:true});}
   if(url==='/api/session')return Response.json({name:'Synthetic admin',isAdmin:adminMode});if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});if(url==='/api/channels')return Response.json({channels});
   return Response.json({companies:[],configured:false});
  };
  const props={nav,filters:ui.defaultFilters,query:'',onCount(){},admin:false};
  await mount(ui.Application);await selectEntity('list','Каналы');
  assert.equal(button('Добавить'),undefined);assert.equal(root.root.findAllByProps({title:'Удалить канал'}).length,0);
  await stop();adminMode=true;await mount(ui.Application);await selectEntity('list','Каналы');
  await click(button('Добавить'));assert.match(label(root.toJSON()),/Новый Telegram-канал/);
  const fields=root.root.findByType('form').findAllByType('input');await change(fields[0],'Second channel');await change(fields[1],'https://t.me/second');
  await selectEntity('list','Каналы');assert.equal(root.root.findAllByType('form').length,1,'Active type click retains draft');
  globalThis.confirm=()=>false;await selectEntity('list','Компании');assert.equal(root.root.findAllByType('form').length,1,'Cancelled navigation retains draft');globalThis.confirm=()=>true;
  failPost=true;await submit();assert.match(label(root.toJSON()),/Synthetic create failure/);assert.equal(root.root.findByType('form').findAllByType('input')[0].props.value,'Second channel');
  failPost=false;await submit();assert.deepEqual(saved,{name:'Second channel',url:'https://t.me/second'});
  assert.equal(root.root.findAllByType('form').length,0);assert.match(label(root.toJSON()),/Second channel/);
  const remove=()=>root.root.findByProps({'aria-label':'Удалить канал «Synthetic channel»'});
  globalThis.confirm=()=>false;await click(remove());assert.equal(deleteCalls,0);
  globalThis.confirm=()=>true;failDelete=true;holdDelete=true;const removeCallback=remove().props.onClick;
  let pending;await act(async()=>{pending=removeCallback();});assert.equal(remove().props.disabled,true);
  await act(async()=>removeCallback());assert.equal(deleteCalls,1,'Repeated delete is guarded');
  await act(async()=>{releaseDelete();await pending;});await flush();assert.match(label(root.toJSON()),/единственным источником/);assert.match(label(root.toJSON()),/Synthetic channel/);assert.equal(remove().props.disabled,false);
  holdDelete=false;failDelete=false;await click(remove());assert.doesNotMatch(label(root.toJSON()),/Synthetic channel/);assert.match(label(root.toJSON()),/Second channel/);
  await selectEntity('list','Компании');await stop();
  const before=requests.length;await mount(ui.Settings,{user:{name:'Synthetic admin',isAdmin:true}});
  assert.doesNotMatch(label(root.toJSON()),/Telegram-каналы/);
  assert.equal(requests.slice(before).filter(r=>r.url==='/api/channels').length,0,'Settings no longer loads channels');
 }finally{await stop();globalThis.fetch=old;}
}
export async function testPromptLimits(){
 const old=fetch;let stored='',revision=0;
 try{
  globalThis.fetch=async(url,options={})=>{
   if(url==='/api/classification/settings')return Response.json({roles:[],maxLength:5000});
   if(options.method==='PUT'){const body=JSON.parse(options.body);stored=body.markdown;revision++;return Response.json({markdown:stored,revision,updatedAt:null});}
   return Response.json({prompts:{cover_letter:{markdown:stored,revision,updatedAt:null},relevance:{markdown:'',revision:0,updatedAt:null},vacancy_extraction:{markdown:'# Extract',revision:0,updatedAt:null},vacancy_role:{markdown:'# Roles',revision:0,updatedAt:null}},maxLengths:{cover_letter:15000,relevance:5000}});
  };
  await mount(ui.Prompts);
  const articles=root.root.findAllByType('article');
  const letter=articles.find(n=>n.findByType('h2').children.includes('Сопроводительное письмо'));
  const relevance=articles.find(n=>n.findByType('h2').children.includes('Релевантность'));
  const save=node=>node.findAllByType('button').find(b=>b.props.type==='submit');
  await change(letter.findByType('textarea'),'😀'.repeat(15000));assert.equal(save(letter).props.disabled,false);
  await change(letter.findByType('textarea'),'😀'.repeat(15001));assert.equal(save(letter).props.disabled,true);
  await change(letter.findByType('textarea'),'😀'.repeat(15000));await act(async()=>letter.findAllByType('form').find(n=>n.props.className?.includes('resume-form')).props.onSubmit({preventDefault(){}}));await flush();assert.equal(Array.from(stored).length,15000);
  await change(relevance.findByType('textarea'),'x'.repeat(5001));assert.equal(save(relevance).props.disabled,true);
  await change(relevance.findByType('textarea'),'x'.repeat(5000));assert.equal(save(relevance).props.disabled,false);
 }finally{await stop();globalThis.fetch=old;}
}
export async function testPromptModels(){
 const old=fetch;let fail=true,revision=0,sent=[],reads=0;
 const price=(inputUsdPerMillion,outputUsdPerMillion)=>({inputUsdPerMillion,outputUsdPerMillion,currency:'USD',tier:'standard',verifiedAt:'2026-10-09',source:'https://developers.openai.com/api/docs/pricing'});
 const models=[{pricing:price(0.1,0.5),id:'gpt-6-luna',name:'GPT-6 Luna',recommended:true,reasoningEfforts:['none','low','medium','high','xhigh','max']},{pricing:price(2,10),id:'gpt-6.1-sol',name:'GPT-6.1 Sol',recommended:true,reasoningEfforts:['low','medium','high','xhigh','max']},{pricing:price(2,8),id:'gpt-4.1',name:'gpt-4.1',recommended:false,reasoningEfforts:[]},{id:'gpt-6.1-luna',name:'gpt-6.1-luna',recommended:false,reasoningEfforts:[],pricing:null}];
 try{
  globalThis.fetch=async(url,options={})=>{
   if(options.method==='PUT'){const body=JSON.parse(options.body);sent.push(body);if(fail)return Response.json({error:'Synthetic config conflict'},{status:409});revision++;return Response.json({...body,revision,kind:'cover_letter',updatedAt:null});}
   reads++;return Response.json({configured:true,key_mask:'••••TEST',models,defaultModel:'gpt-6-luna',warning:'GPT-6.1 Luna отсутствует'});
  };
  await mount(ui.PromptModel,{initial:{kind:'cover_letter',provider:'openrouter',openaiModel:'',reasoningEffort:'medium',revision:0,updatedAt:null}});
  assert.equal(reads,0,'OpenRouter selection does not preload OpenAI models');assert.equal(root.root.findAllByType('select').length,1);assert.ok(!label(root.toJSON()).includes('Примерная стоимость 1000 генераций'));
  await change(root.root.findByProps({'aria-label':'Провайдер'}),'openai');await flush();
  assert.equal(reads,1);assert.equal(root.root.findByProps({'aria-label':'Модель OpenAI'}).props.value,'gpt-6-luna');assert.equal(root.root.findByProps({'aria-label':'Уровень размышлений'}).props.value,'medium');
  assert.match(label(root.root.findByProps({className:'prompt-cost'})),/≈ 191,25 ₽/);assert.match(label(root.toJSON()),/условно 2 символа на токен/);assert.match(label(root.root.findByProps({className:'prompt-cost'})),/за 1000 генераций/);assert.match(label(root.root.findByProps({className:'prompt-cost'})),/Фиксированный курс: 1 \$ = 85 ₽/);assert.match(label(root.root.findByProps({className:'prompt-cost'})),/Вход: 85,00 ₽; текст ответа: 106,25 ₽/);assert.match(label(root.toJSON()),/Токены размышлений/);assert.match(label(root.root.findByProps({'aria-label':'Модель OpenAI'})),/\$0,50 \/ 1 млн выходных токенов/);
  assert.equal(root.root.findAllByType('optgroup').length,2);assert.match(label(root.toJSON()),/6.1 Luna отсутствует/);
  await submit();assert.match(label(root.toJSON()),/Synthetic config conflict/);assert.equal(root.root.findByProps({'aria-label':'Модель OpenAI'}).props.value,'gpt-6-luna');assert.equal(sent[0].revision,0);
  fail=false;await submit();assert.equal(sent[1].revision,0);assert.equal(button('Сохранить модель').props.disabled,true);
  await change(root.root.findByProps({'aria-label':'Модель OpenAI'}),'gpt-6.1-sol');assert.ok(!root.root.findByProps({'aria-label':'Уровень размышлений'}).findAllByType('option').some(o=>o.props.value==='none'));
  assert.match(label(root.root.findByProps({className:'prompt-cost'})),/≈ 3\s825,00 ₽/);
  await change(root.root.findByProps({'aria-label':'Уровень размышлений'}),'high');assert.match(label(root.root.findByProps({className:'prompt-cost'})),/≈ 3\s825,00 ₽/,'Effort cannot invent a reasoning budget');await submit();assert.equal(sent.at(-1).revision,1);assert.equal(sent.at(-1).reasoningEffort,'high');
  await change(root.root.findByProps({'aria-label':'Модель OpenAI'}),'gpt-4.1');assert.equal(root.root.findByProps({'aria-label':'Уровень размышлений'}).props.value,'auto');
  assert.match(label(root.root.findByProps({className:'prompt-cost'})),/≈ 3\s400,00 ₽/);
  await change(root.root.findByProps({'aria-label':'Модель OpenAI'}),'gpt-6.1-luna');assert.match(label(root.root.findByProps({className:'prompt-cost'})),/стоимость не рассчитана/);assert.ok(!label(root.root.findByProps({className:'prompt-cost'})).includes('$'));
  await change(root.root.findByProps({'aria-label':'Провайдер'}),'openrouter');assert.equal(root.root.findAllByType('select').length,1);assert.equal(root.root.findAllByProps({className:'prompt-cost'}).length,0);await submit();assert.equal(sent.at(-1).provider,'openrouter');
  await stop();await mount(ui.PromptModel,{initial:{kind:'relevance',provider:'openai',openaiModel:'gpt-6-luna',reasoningEffort:'medium',revision:0,updatedAt:null}});await flush();assert.match(label(root.root.findByProps({className:'prompt-cost'})),/≈ 63,79 ₽/);assert.match(label(root.root.findByProps({className:'prompt-cost'})),/15 000 русских символов/);assert.match(label(root.root.findByProps({className:'prompt-cost'})),/1 \$ = 85 ₽/);
 }finally{await stop();globalThis.fetch=old;}
}
export async function testOpenAIKey(){
 const old=fetch;let configured=false,fail=true,saved=[],modelReads=0;
 try{
  globalThis.fetch=async(url,options={})=>{
   if(options.method==='PUT'){const body=JSON.parse(options.body);saved.push(body);if(fail)return Response.json({error:'Synthetic key failure'},{status:400});configured=true;return Response.json({configured:true,key_mask:'••••TEST'});}
   modelReads++;return Response.json({configured,key_mask:configured?'••••TEST':'',models:configured?[{id:'gpt-6-luna',name:'GPT-6 Luna',recommended:true,reasoningEfforts:[],pricing:{inputUsdPerMillion:0.1,outputUsdPerMillion:0.5,currency:'USD',tier:'standard',verifiedAt:'2026-10-09',source:'https://developers.openai.com/api/docs/pricing'}},{id:'gpt-6.1-luna',name:'gpt-6.1-luna',recommended:false,reasoningEfforts:[],pricing:null}]:[],defaultModel:'',warning:'Synthetic warning'});
  };
  await mount(ui.OpenAI);const key=()=>root.root.findByProps({id:'openaiKey'});assert.equal(key().props.type,'password');assert.equal(key().props.autoComplete,'off');
  assert.match(label(root.toJSON()),/Автоматическая проверка баланса недоступна/);const billing=root.root.findByProps({href:'https://platform.openai.com/settings/organization/billing/overview'});assert.equal(billing.props.rel,'noopener noreferrer');assert.equal(billing.props.target,'_blank');assert.equal(modelReads,1,'No per-model price or billing requests');
  const secret='sk-proj-SYNTHETIC-KEY-TEST';await change(key(),secret);await submit();assert.equal(key().props.value,secret);assert.match(label(root.toJSON()),/Synthetic key failure/);
  fail=false;await submit();assert.deepEqual(saved.at(-1),{apiKey:secret});assert.equal(key().props.value,'');assert.match(key().props.placeholder,/••••TEST/);assert.ok(!JSON.stringify(root.toJSON()).includes(secret));assert.ok(modelReads>=2,'Key save invalidates OpenAI model/status cache');assert.match(label(root.root.findByProps({className:'or-table openai-table'})),/\$0,50/);assert.match(label(root.toJSON()),/Цена не подтверждена/);
 }finally{await stop();globalThis.fetch=old;}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(process.argv[1]).href){for(const test of [testCache,testInitialLoading,testRevisionEditor,testTaskSchedule,testVacancyEditor,testCoverLetter,testDrawerTabs,testReviewDrawer,testCatalogRows,testPersistedFavoriteFilter,testFilterMenuFocus,testPanelLoading,testChannelManagement,testPromptLimits,testPromptModels,testOpenAIKey]){await test();console.log('PASS '+test.name);}}
