import {vacancyTestPayload} from './test-support.mjs';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();const backend=await import('../.tests/backend.mjs');
const {agencySalaryRangeStatements}=await moduleFor('server/repositories/agency-salaries.js');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'agency-atomicity'},bindings:{SITE_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('External network disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 const catalog=backend.normalizeCatalog({...seed,companies:[{...seed.companies[0],name:'Atomic company',jobs:[]}],agencies:[
   {name:'Agency A',middle:[110000,220000],senior:[null,null],lead:[null,500000],head:[null,null],salaryRangeSource:{senior:'vacancies'},workingGuide:'Keep guide'},
   {name:'Agency B',middle:[null,null],senior:[300000,400000],salaryRangeSource:{senior:'vacancies'}},
   {name:'Agency C',middle:['100000','200000'],senior:[0,0],salaryRangeSource:{middle:'manual',senior:'manual'}},
   {name:'Agency D',senior:[null,null],salaryRangeSource:{senior:'vacancies'}}
 ]});
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 await db.prepare("UPDATE app_catalog SET data_json=json_set(data_json,'$.agencies[2].middle',json('[\"100000\",\"200000\"]')) WHERE id=1").run();
 for(const name of ['Atomic company',...catalog.agencies.map(a=>a.name)])await db.batch([db.prepare('INSERT INTO company_crm(company_name) VALUES(?)').bind(name),db.prepare('INSERT INTO company_vacancy_state(company_name) VALUES(?)').bind(name)]);
 const fixtures=[['Agency A','Senior',200000,300000,0],['Agency A','Senior',250000,450000,1],['Agency A','Head',null,800000,0],['Agency A','Middle',500000,600000,0],['Agency A','Lead',300000,700000,0],['Agency B','Senior',350000,550000,0],['Agency C','Senior',0,0,0]];
 for(let i=0;i<fixtures.length;i++){const [owner,level,lo,hi,archived]=fixtures[i];await db.prepare('INSERT INTO company_vacancies(company_name,entity_type,url,title,role,level,description_md,salary_from,salary_to,archived) VALUES(?,?,?,?,?,?,?,?,?,?)').bind(owner,'agency','https://atomic.test/'+i,'Original '+i,'Менеджер проектов',level,'Full',lo,hi,archived).run();}
 const login=await mf.dispatchFetch('https://atomic.test/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});const cookie=login.headers.get('set-cookie').split(';')[0];
 const req=async (id,body)=>{body=await vacancyTestPayload(()=>mf.dispatchFetch('https://atomic.test/api/vacancies/'+id,{headers:{cookie}}),'/api/vacancies/'+id,'PUT',body);return mf.dispatchFetch('https://atomic.test/api/vacancies/'+id,{method:'PUT',headers:{cookie,origin:'https://atomic.test','content-type':'application/json'},body:JSON.stringify(body)})};
 const rows=(await db.prepare('SELECT * FROM company_vacancies ORDER BY id').all()).results;
 const payload=row=>({companyName:row.company_name,entityType:row.entity_type,url:row.url,title:row.title,role:row.role,level:row.level,descriptionMd:row.description_md,salaryFrom:row.salary_from,salaryTo:row.salary_to,relevance:77});
 const readCatalogue=async()=>JSON.parse((await db.prepare('SELECT data_json FROM app_catalog WHERE id=1').first()).data_json);
 const snapshot=async()=>Promise.all(['app_catalog','company_vacancies','company_vacancy_user_state'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 const before=await snapshot();await db.prepare("CREATE TRIGGER fail_catalogue BEFORE UPDATE ON app_catalog BEGIN SELECT RAISE(ABORT,'synthetic catalogue failure'); END").run();
 let r=await req(rows[0].id,{...payload(rows[0]),salaryFrom:600000,salaryTo:700000});assert.equal(r.status,400,await r.clone().text());assert.deepEqual(await snapshot(),before,'Catalogue failure must roll back vacancy and relevance too');
 await db.prepare('DROP TRIGGER fail_catalogue').run();
 const priorCatalogue=await readCatalogue();r=await req(rows[0].id,{...payload(rows[0]),salaryFrom:600000,salaryTo:700000});assert.equal(r.status,200,await r.clone().text());const sqlCatalogue=await readCatalogue();
 // Compare salary semantics against the existing JS calculator on identical stored rows.
 await db.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').bind(JSON.stringify(priorCatalogue)).run();
 await backend.recalculateAgencySalaryRanges(db);const reference=await backend.readAppCatalog(db),actual=backend.normalizeCatalog(sqlCatalogue);
 for(let i=0;i<reference.agencies.length;i++)for(const key of ['middle','senior','lead','head','salaryRangeSource'])assert.deepEqual(actual.agencies[i][key],reference.agencies[i][key],reference.agencies[i].name+' '+key);
 assert.deepEqual(sqlCatalogue.companies,priorCatalogue.companies);assert.equal(sqlCatalogue.agencies[0].workingGuide,'Keep guide');assert.deepEqual(actual.agencies[0].middle,[110000,220000]);assert.deepEqual(actual.agencies[0].lead,[null,500000]);
 await db.prepare('UPDATE app_catalog SET data_json=? WHERE id=1').bind(JSON.stringify(sqlCatalogue)).run();
 // Prepared calculation uses current SQL data; it cannot write an old catalogue snapshot.
 const prepared=agencySalaryRangeStatements(db,actual);
 await db.prepare("UPDATE app_catalog SET data_json=json_set(data_json,'$.companies[0].sector','Concurrent sector','$.agencies[0].workingGuide','Concurrent guide','$.agencies[0].middle',json('[150000,250000]')) WHERE id=1").run();
 await db.batch(prepared);let latest=await readCatalogue();assert.equal(latest.companies[0].sector,'Concurrent sector');assert.equal(latest.agencies[0].workingGuide,'Concurrent guide');assert.deepEqual(latest.agencies[0].middle,[150000,250000]);
 // Both separate vacancy edits contribute to the final range despite concurrent preflight reads.
 const parallel=await Promise.all([req(rows[0].id,{...payload(rows[0]),salaryFrom:500000,salaryTo:600000}),req(rows[1].id,{...payload(rows[1]),salaryFrom:150000,salaryTo:900000})]);for(const response of parallel)assert.equal(response.status,200,await response.clone().text());
 latest=await readCatalogue();assert.deepEqual(latest.agencies[0].senior,[150000,900000]);assert.equal(latest.agencies[0].workingGuide,'Concurrent guide');
 // Moving out of an agency removes its contribution; empty derived ranges reset.
 r=await req(rows[5].id,{...payload(rows[5]),companyName:'Atomic company',entityType:'company'});assert.equal(r.status,200,await r.clone().text());latest=await readCatalogue();assert.deepEqual(latest.agencies[1].senior,[null,null]);assert.equal(latest.agencies[1].salaryRangeSource.senior,'manual');
 const unchanged=await snapshot();await db.batch(agencySalaryRangeStatements(db,backend.normalizeCatalog(latest)));assert.deepEqual(await snapshot(),unchanged,'Unchanged ranges must not rewrite catalogue');
 console.log('D1/workerd agency salaries: rollback, JS parity for all levels/manual/zero/null/numeric strings/archived, current catalogue preservation, concurrent vacancy edits, owner transfer and no-op passed.');
} finally {await mf.dispose();}
