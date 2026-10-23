import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {prepareBackend,moduleFor,vacancyTestPayload} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'channel-safety'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('External network disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
let root;const oldFetch=fetch,oldConfirm=globalThis.confirm;
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Safety company',jobs:[]}],agencies:[{name:'Safety agency'}]})).run();
 const cookies={};for(const name of ['Eugene','Nastya']) {const r=await mf.dispatchFetch('https://safety.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=async (path,method='GET',body,user='Eugene')=>{body=await vacancyTestPayload(()=>req(path,'GET',undefined,user),path,method,body);return mf.dispatchFetch('https://safety.test'+path,{method,headers:{cookie:cookies[user],origin:'https://safety.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})})};
 const channel=async n=>{const r=await req('/api/channels','POST',{name:'Safety '+n,url:'https://t.me/safety_'+n});assert.equal(r.status,201);return (await r.json()).channelId;};
 const input={entityType:'channel',companyName:'',title:'Safety vacancy',url:'https://safety.test/vacancy',role:'Менеджер проектов',level:'Senior',descriptionMd:'Synthetic body'};
 const vacancy=async(channelId,n,extra={})=>{const r=await req('/api/vacancies','POST',{...input,url:'https://safety.test/vacancy/'+n,telegramChannelId:channelId,telegramMessageUrl:'https://t.me/safety_'+n+'/1',telegramContact:'@keep_contact',...extra});assert.equal(r.status,201,await r.clone().text());return (await r.json()).vacancies.find(v=>v.url==='https://safety.test/vacancy/'+n);};
 const first=await channel('first'),v=await vacancy(first,'first');
 await db.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').bind('Eugene',v.id,'Keep subject','Keep letter').run();
 await db.prepare('INSERT INTO company_applications(user_name,company_name,vacancy_id,status) VALUES(?,?,?,?)').bind('Nastya',v.companyName,v.id,'Скрининг').run();
 await db.prepare('INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at) VALUES(?,?,?,?,?,?)').bind('Nastya',v.companyName,v.id,'call','Keep task','2026-10-01T12:00:00.000Z').run();
 const tables=['telegram_channels','vacancy_telegram_sources','company_vacancies','vacancy_cover_letters','company_applications','company_tasks'];
 const snapshot=async()=>Promise.all(tables.map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 const before=await snapshot();let r=await req('/api/channels/'+first,'DELETE');assert.equal(r.status,409);assert.match((await r.json()).error,/единственным источником/);assert.deepEqual(await snapshot(),before);
 assert.equal((await req('/api/channels/'+first,'DELETE',undefined,'Nastya')).status,403);
 assert.equal((await req('/api/channels','POST',{name:'Denied',url:'https://t.me/denied'},'Nastya')).status,403);
 assert.equal((await req('/api/channels/999999','DELETE')).status,404);
 const models=await moduleFor('frontend/features/vacancies/model.ts'),{defaultFilters}=await moduleFor('frontend/features/companies/Filters.tsx');
 const list=await (await req('/api/vacancies/list')).json();assert.equal(models.visibleVacancy(list.vacancies.find(row=>row.id===v.id),'channel',new Map(list.entities.map(e=>[e.name,e])),defaultFilters,models.defaultVacancyFilters,''),true);
 // Actual admin channel list displays the server conflict and keeps the row.
 const {default:CompanyList}=await moduleFor('frontend/features/companies/CompanyList.tsx');globalThis.IS_REACT_ACT_ENVIRONMENT=true;globalThis.confirm=()=>true;
 globalThis.fetch=(url,o={})=>req(url,o.method||'GET',o.body?JSON.parse(o.body):undefined);
 await act(async()=>{root=create(h(CompanyList,{nav:{openCompany(){},openVacancy(){},openChannel(){}},filters:defaultFilters,query:'',onCount(){},admin:true,type:'channel'}));});
 for(let i=0;i<30&&!root.root.findAllByProps({title:'Удалить канал'}).length;i++)await act(async()=>{await new Promise(resolve=>setTimeout(resolve,5));});
 assert.equal(root.root.findAllByProps({title:'Удалить канал'}).length,1);
 await act(async()=>{await root.root.findByProps({title:'Удалить канал'}).props.onClick();});
 assert.match(JSON.stringify(root.toJSON()),/единственным источником/);assert.equal(root.root.findAllByProps({title:'Удалить канал'}).length,1);
 await act(async()=>root.unmount());root=null;globalThis.fetch=oldFetch;
 // A second source allows removing one channel; the remaining source then becomes protected.
 const second=await channel('second');
 r=await req('/api/vacancies/'+v.id,'PUT',{...input,companyName:v.companyName,url:v.url,telegramChannelId:second,telegramMessageUrl:'https://t.me/safety_second/1'});assert.equal(r.status,200,await r.clone().text());
 assert.equal((await req('/api/channels/'+first,'DELETE')).status,200);assert.equal((await req('/api/channels/'+second,'DELETE')).status,409);
 assert.equal((await db.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=?').bind(v.id).first()).n,1);
 assert.equal((await db.prepare('SELECT count(*) n FROM company_applications WHERE vacancy_id=?').bind(v.id).first()).n,1);
 assert.equal((await db.prepare('SELECT count(*) n FROM company_tasks WHERE vacancy_id=?').bind(v.id).first()).n,1);
 // Company-owned vacancy remains accessible after its optional source is removed.
 const companyChannel=await channel('company');const companyVacancy=await vacancy(companyChannel,'company',{entityType:'company',companyName:'Safety company'});
 assert.equal((await req('/api/channels/'+companyChannel,'DELETE')).status,200);
 const companyList=await (await req('/api/vacancies/list')).json();assert.equal(models.visibleVacancy(companyList.vacancies.find(row=>row.id===companyVacancy.id),'company',new Map(companyList.entities.map(e=>[e.name,e])),defaultFilters,models.defaultVacancyFilters,''),true);
 // Agency-owned vacancy also survives removal of its optional source.
 const agencyChannel=await channel('agency');const agencyVacancy=await vacancy(agencyChannel,'agency',{entityType:'agency',companyName:'Safety agency'});
 assert.equal((await req('/api/channels/'+agencyChannel,'DELETE')).status,200);
 const agencyList=await (await req('/api/vacancies/list')).json();assert.equal(models.visibleVacancy(agencyList.vacancies.find(row=>row.id===agencyVacancy.id),'agency',new Map(agencyList.entities.map(e=>[e.name,e])),defaultFilters,models.defaultVacancyFilters,''),true);
 // Two concurrent deletions of a shared pair cannot detach both sources.
 const left=await channel('left'),right=await channel('right'),concurrentVacancy=await vacancy(left,'left');
 r=await req('/api/vacancies/'+concurrentVacancy.id,'PUT',{...input,url:concurrentVacancy.url,companyName:concurrentVacancy.companyName,telegramChannelId:right,telegramMessageUrl:'https://t.me/safety_right/1'});assert.equal(r.status,200);
 const results=await Promise.all([req('/api/channels/'+left,'DELETE'),req('/api/channels/'+right,'DELETE')]);assert.deepEqual(results.map(r=>r.status).sort(),[200,409]);
 assert.equal((await db.prepare('SELECT count(*) n FROM vacancy_telegram_sources WHERE vacancy_id=?').bind(concurrentVacancy.id).first()).n,1);
 console.log('D1/workerd + React: last source returns 409 without changes, UI retains row/error, multiple sources, company/agency visibility, concurrent deletions and preserved letters/applications/tasks passed.');
} finally {if(root)await act(async()=>root.unmount());globalThis.fetch=oldFetch;globalThis.confirm=oldConfirm;await mf.dispose();}
