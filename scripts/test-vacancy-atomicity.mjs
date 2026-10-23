import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend,vacancyTestPayload,applicationTestIntent} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'vacancy-atomicity'},bindings:{SITE_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('External network disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 const catalog={...seed,companies:['Atomic A','Atomic B'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[{name:'Atomic agency'}]};
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 for(const c of catalog.companies)await db.batch([db.prepare('INSERT INTO company_crm(company_name) VALUES(?)').bind(c.name),db.prepare('INSERT INTO company_vacancy_state(company_name) VALUES(?)').bind(c.name)]);
 const login=await mf.dispatchFetch('https://atomic.test/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});assert.equal(login.status,303);const cookie=login.headers.get('set-cookie').split(';')[0];
 const req=async (path,method='GET',body)=>{body=await vacancyTestPayload(()=>req(path,'GET',undefined),path,method,body);return mf.dispatchFetch('https://atomic.test'+path,{method,headers:{cookie,origin:'https://atomic.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})})};
 let r=await req('/api/channels','POST',{name:'Atomic source',url:'https://t.me/atomic_source'});assert.equal(r.status,201);const channelId=(await r.json()).channelId;
 const input={companyName:'Atomic A',entityType:'company',url:'https://atomic.test/vacancy',title:'Original',role:'Менеджер проектов',level:'Senior',descriptionMd:'Original body',salaryFrom:200000,salaryTo:300000,relevance:50,telegramChannelId:channelId,telegramMessageUrl:'https://t.me/atomic_source/1',telegramContact:'@original_contact'};
 r=await req('/api/vacancies','POST',input);assert.equal(r.status,201,await r.clone().text());const id=(await r.json()).vacancies.find(v=>v.url===input.url).id;
 assert.equal((await req('/api/vacancies/'+id+'/apply','POST',{companyName:input.companyName,...await applicationTestIntent(()=>req('/api/applications/create-context'))})).status,200);
 await db.prepare('INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at) VALUES(?,?,?,?,?,?)').bind('Eugene','Atomic A',id,'call','Keep','2026-10-01T12:00:00.000Z').run();
 await db.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').bind('Eugene',id,'Original subject','Original letter').run();
 await db.prepare('INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,favorite) VALUES(?,?,?,?)').bind('Nastya',id,42,1).run();
 const tables=['company_vacancies','company_applications','company_tasks','vacancy_cover_letters','vacancy_telegram_sources','company_vacancy_user_state','app_catalog'];
 const snapshot=async()=>Promise.all(tables.map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY '+(table==='company_vacancy_user_state'?'user_name,vacancy_id':'rowid')).all()).results));
 const before=await snapshot();
 const edited={...input,companyName:'Atomic B',title:'Changed',relevance:88,telegramMessageUrl:'https://t.me/atomic_source/2',telegramContact:'@changed_contact',coverLetterScope:'personal',coverLetterSubject:'Changed subject',coverLetterMd:'Changed letter'};
 // Fail each core write in real D1 and compare every affected table, not only the title.
 for(const [table,event] of [['company_vacancies','UPDATE'],['vacancy_cover_letters','INSERT'],['company_applications','UPDATE'],['company_tasks','UPDATE'],['vacancy_telegram_sources','INSERT'],['company_vacancy_user_state','INSERT']]) {
   await db.prepare(`CREATE TRIGGER fail_mutation BEFORE ${event} ON ${table} BEGIN SELECT RAISE(ABORT,'synthetic ${table} failure'); END`).run();
   r=await req('/api/vacancies/'+id,'PUT',edited);assert.equal(r.status,400,table);assert.deepEqual(await snapshot(),before,table+' must roll back all writes');
   await db.prepare('DROP TRIGGER fail_mutation').run();
 }
 for(const table of ['vacancy_telegram_sources','vacancy_cover_letters']) {
   await db.prepare(`CREATE TRIGGER fail_mutation BEFORE DELETE ON ${table} BEGIN SELECT RAISE(ABORT,'synthetic deletion failure'); END`).run();
   r=await req('/api/vacancies/'+id,'PUT',{...edited,telegramChannelId:null,coverLetterSubject:'',coverLetterMd:''});assert.equal(r.status,400,table);assert.deepEqual(await snapshot(),before,table+' deletion rollback');
   await db.prepare('DROP TRIGGER fail_mutation').run();
 }
 r=await req('/api/vacancies/'+id,'PUT',edited);assert.equal(r.status,200,await r.clone().text());
 const [vacancies,applications,tasks,letters,sources,states]=await snapshot();
 assert.equal(vacancies.find(v=>v.id===id).title,'Changed');assert.equal(vacancies.find(v=>v.id===id).company_name,'Atomic B');
 assert.equal(applications[0].company_name,'Atomic B');assert.equal(tasks[0].company_name,'Atomic B');assert.equal(tasks[0].note,'Keep');
 assert.equal(letters[0].markdown,'Changed letter');assert.equal(sources[0].message_url,'https://t.me/atomic_source/2');
 assert.equal(states.find(s=>s.user_name==='Eugene').relevance,88);assert.equal(states.find(s=>s.user_name==='Nastya').relevance,42);assert.equal(states.find(s=>s.user_name==='Nastya').favorite,1);
 // R03a: CREATE and all dependants must roll back together, with no compensating DELETE.
 const fresh={...input,companyName:'Atomic agency',entityType:'agency',url:'https://atomic.test/new-agency',telegramMessageUrl:'https://t.me/atomic_source/3'};
 const beforeCreate=await snapshot();
 await db.prepare("CREATE TRIGGER forbid_cleanup BEFORE DELETE ON company_vacancies BEGIN SELECT RAISE(ABORT,'cleanup must not be needed'); END").run();
 for(const [table,event] of [['company_vacancies','INSERT'],['vacancy_telegram_sources','INSERT'],['company_vacancy_user_state','INSERT'],['app_catalog','UPDATE']]) {
   await db.prepare(`CREATE TRIGGER fail_create BEFORE ${event} ON ${table} BEGIN SELECT RAISE(ABORT,'synthetic create failure'); END`).run();
   r=await req('/api/vacancies','POST',fresh);assert.equal(r.status,400,table);assert.deepEqual(await snapshot(),beforeCreate,table+' create rollback');
   await db.prepare('DROP TRIGGER fail_create').run();
 }
 await db.prepare('DROP TRIGGER forbid_cleanup').run();
 r=await req('/api/vacancies','POST',fresh);assert.equal(r.status,201,await r.clone().text());const created=(await r.json()).vacancies.find(v=>v.url===fresh.url);assert.ok(created);
 assert.equal((await db.prepare('SELECT vacancy_id FROM vacancy_telegram_sources WHERE message_url=?').bind(fresh.telegramMessageUrl).first()).vacancy_id,created.id);
 assert.equal((await db.prepare('SELECT relevance FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=?').bind('Eugene',created.id).first()).relevance,fresh.relevance);
 // Parallel new rows resolve their own IDs by unique URL in their transaction.
 const parallel=await Promise.all([4,5].map(n=>req('/api/vacancies','POST',{...fresh,url:'https://atomic.test/new/'+n,telegramMessageUrl:'https://t.me/atomic_source/'+n,salaryFrom:n===4?100000:150000,salaryTo:n===4?500000:600000})));
 for(const response of parallel)assert.equal(response.status,201,await response.clone().text());
 for(const n of [4,5]) {const row=await db.prepare('SELECT id FROM company_vacancies WHERE url=?').bind('https://atomic.test/new/'+n).first();assert.equal((await db.prepare('SELECT vacancy_id FROM vacancy_telegram_sources WHERE message_url=?').bind('https://t.me/atomic_source/'+n).first()).vacancy_id,row.id);}
 const currentCatalogue=JSON.parse((await db.prepare('SELECT data_json FROM app_catalog WHERE id=1').first()).data_json);assert.deepEqual(currentCatalogue.agencies[0].senior,[100000,600000]);
 const duplicateInput={...fresh,url:'https://atomic.test/duplicate',telegramMessageUrl:'https://t.me/atomic_source/6'};
 const duplicates=await Promise.all([req('/api/vacancies','POST',duplicateInput),req('/api/vacancies','POST',duplicateInput)]);assert.deepEqual(duplicates.map(r=>r.status).sort(),[201,400]);
 assert.equal((await db.prepare('SELECT count(*) n FROM company_vacancies WHERE url=?').bind(duplicateInput.url).first()).n,1);
 assert.equal((await db.prepare('SELECT count(*) n FROM vacancy_telegram_sources WHERE message_url=?').bind(duplicateInput.telegramMessageUrl).first()).n,1);
 console.log('D1/workerd: PUT/POST rollback at every core write, no cleanup needed, sources/new IDs/relevance, agency catalogue, successful retry, parallel creation/duplicate URLs and other user state passed.');
} finally {await mf.dispose();}
