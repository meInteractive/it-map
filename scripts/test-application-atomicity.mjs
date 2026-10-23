// Real D1/workerd regression: no mock transaction or last_insert_rowid semantics.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend,applicationTestIntent} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'atomicity-synthetic'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'atomicity-synthetic-secret'},outboundService:()=>new Response('External network disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 const catalog={...seed,companies:['Atomic A','Atomic B','Atomic C'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[]};
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 const cookies={};for(const name of ['Eugene','Nastya']) {const r=await mf.dispatchFetch('https://atomic.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(body,user='Eugene')=>mf.dispatchFetch('https://atomic.test/api/applications',{method:'POST',headers:{cookie:cookies[user],origin:'https://atomic.test','content-type':'application/json'},body:JSON.stringify(body)});
 const intent=(user='Eugene')=>applicationTestIntent(()=>mf.dispatchFetch('https://atomic.test/api/applications/create-context',{headers:{cookie:cookies[user]}}));
 const input={companyName:'Atomic A',vacancyId:null,status:'Откликнулся',comment:'Synthetic retry',applicationSources:['hh'],...await intent()};
 const snapshot=async()=>Promise.all(['company_applications','company_application_status_history','application_creation_receipts'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 await db.prepare("CREATE TRIGGER fail_history BEFORE INSERT ON company_application_status_history BEGIN SELECT RAISE(ABORT,'synthetic history failure'); END").run();
 const before=await snapshot();assert.equal((await req(input)).status,503);assert.deepEqual(await snapshot(),before,'Failed history must roll application back');
 await db.prepare('DROP TRIGGER fail_history').run();
 let r=await req(input);assert.equal(r.status,201,await r.clone().text());
 let [apps,history]=await snapshot();assert.equal(apps.length,1);assert.equal(history.length,1);assert.equal(history[0].application_id,apps[0].id);assert.equal(history[0].to_status,apps[0].status);
 // Empty initial status intentionally has no history. Parallel creations keep correct IDs.
 r=await req({...input,...await intent(),status:'',comment:'No history'});assert.equal(r.status,201);
 const statuses=['Скрининг','Офер'];
 const parallel=await Promise.all(statuses.map(async status=>req({...input,...await intent(),status,comment:status})));for(const response of parallel)assert.equal(response.status,201,await response.clone().text());
 [apps,history]=await snapshot();assert.equal(apps.length,4);assert.equal(history.length,3);
 for(const app of apps) {const entries=history.filter(h=>h.application_id===app.id);assert.equal(entries.length,app.status?1:0);if(app.status)assert.equal(entries[0].to_status,app.status);}
 // Unique user/vacancy constraint under two requests; another user may apply independently.
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Atomic B') ON CONFLICT DO NOTHING").run();
 await db.prepare("INSERT INTO company_vacancies(company_name,url,title) VALUES('Atomic B','https://atomic.test/vacancy','Synthetic')").run();
 const vacancy=await db.prepare("SELECT id FROM company_vacancies WHERE url='https://atomic.test/vacancy'").first();
 const linked={...input,companyName:'Atomic B',vacancyId:vacancy.id};
 const duplicateIntents=await Promise.all([intent(),intent()]);const duplicate=await Promise.all(duplicateIntents.map(value=>req({...linked,...value})));assert.equal(duplicate.filter(r=>r.status===201).length,1);assert.ok(duplicate.some(r=>[400,503].includes(r.status)));
 assert.equal((await req({...linked,...await intent('Nastya')},'Nastya')).status,201);
 // Existing DB trigger guards the tenth slot even if both API prechecks see nine.
 const limited={...input,companyName:'Atomic C'};
 for(let i=0;i<9;i++)assert.equal((await req({...limited,...await intent(),comment:String(i)})).status,201);
 const lastIntents=await Promise.all([intent(),intent()]);const last=await Promise.all(lastIntents.map(value=>req({...limited,...value})));assert.equal(last.filter(r=>r.status===201).length,1);assert.ok(last.some(r=>[400,503].includes(r.status)));
 assert.equal((await db.prepare("SELECT count(*) n FROM company_applications WHERE company_name='Atomic C' AND user_name='Eugene'").first()).n,10);
 const mismatch=await db.prepare(`SELECT count(*) n FROM company_applications a LEFT JOIN company_application_status_history h ON h.application_id=a.id
   WHERE (a.status<>'' AND (h.id IS NULL OR h.to_status<>a.status)) OR (a.status='' AND h.id IS NOT NULL)`).first();assert.equal(mismatch.n,0);
 assert.equal((await db.prepare('SELECT count(*) n FROM company_application_status_history h LEFT JOIN company_applications a ON a.id=h.application_id WHERE a.id IS NULL').first()).n,0);
 console.log('D1/workerd: failed history rolls back application, retry has one record, empty status, parallel history IDs, user/vacancy uniqueness and tenth-slot limit passed. Lost HTTP-response replay is covered by the dedicated creation HTTP regression.');
} finally {await mf.dispose();}
