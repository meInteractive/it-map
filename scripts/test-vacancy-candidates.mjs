import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?vacancy-roles'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



try{
 assert.equal((await request('/api/vacancy-candidates','GET',undefined,user)).status,403);
 assert.equal((await request('/api/vacancy-candidates','POST',{titleRaw:'x'},user)).status,403);
 assert.equal((await worker.fetch(new Request(base+'/api/vacancy-candidates',{method:'POST',headers:{cookie:admin,origin:'https://evil.invalid'},body:'{}'}),env)).status,403);
 await request('/api/companies/list');const companyId=sqlite.prepare('SELECT id FROM company_identity WHERE company_name=?').get(company).id;
 const input={titleRaw:'  Senior Engineering Manager  ',companyId,companyNameRaw:company,vacancyUrl:'https://jobs.example/vacancy?job_id=42&utm_source=test#details',sourceId:'companySite',sourceUrl:'https://jobs.example/jobs',acquisitionMethod:'manual'};
 let response=await request('/api/vacancy-candidates','POST',input);assert.equal(response.status,201,await response.clone().text());let saved=await response.json();const id=saved.id;
 assert.equal(saved.candidate.titleRaw,input.titleRaw);assert.equal(saved.candidate.canonicalUrl,'https://jobs.example/vacancy?job_id=42');assert.equal(saved.candidate.processingStatus,'pending_classification');
 response=await request('/api/vacancy-candidates','POST',{...input,vacancyUrl:'https://jobs.example/vacancy?utm_campaign=x&job_id=42'});assert.equal(response.status,200);saved=await response.json();assert.equal(saved.id,id);assert.equal(saved.created,false);assert.equal(saved.candidate.firstSeenAt,saved.candidate.createdAt);
 response=await request('/api/vacancy-candidates','POST',{...input,externalJobId:'external-42'});assert.equal((await response.json()).id,id,'URL identity can be enriched with external ID');
 response=await request('/api/vacancy-candidates','POST',{...input,externalJobId:'external-42',vacancyUrl:'https://jobs.example/new-location?id=42',titleRaw:'New title'});saved=await response.json();assert.equal(saved.id,id);assert.equal(saved.candidate.titleRevision,2);assert.ok(saved.candidate.titleUpdatedAt);
 assert.equal((await (await request('/api/vacancy-candidates','POST',{...input,titleRaw:'New title'})).json()).id,id,'old URL alias survives');
 assert.equal((await request('/api/vacancy-candidates','POST',{...input,externalJobId:'another-external'})).status,409,'ambiguous reused URL never silently joins external publications');
 assert.equal((await request('/api/vacancy-candidates','POST',{titleRaw:'Same title'})).status,400);
 for(const value of ['javascript:alert(1)','https://user:password@example.test/job'])assert.equal((await request('/api/vacancy-candidates','POST',{...input,vacancyUrl:value})).status,400);
 const many=await Promise.all(Array.from({length:20},()=>request('/api/vacancy-candidates','POST',{titleRaw:'Repeated concurrently',vacancyUrl:'https://jobs.example/concurrent'}).then(async r=>{assert.ok([200,201].includes(r.status),await r.clone().text());return r.json();})));
 assert.equal(new Set(many.map(r=>r.id)).size,1);assert.equal(many.filter(r=>r.created).length,1);
 for(const sourceNamespace of ['feed-one','feed-two']){response=await request('/api/vacancy-candidates','POST',{titleRaw:'Same title',sourceNamespace,externalJobId:'123',acquisitionMethod:'excel'});assert.equal(response.status,201);}
 const publishers=[];
 for(const publisher of ['publisher-one','publisher-two']){const r=await request('/api/vacancy-candidates','POST',{titleRaw:'Equal external IDs',externalJobId:'42',sourceUrl:'https://employer.example/jobs',vacancyUrl:'https://'+publisher+'.example/job/42'});assert.equal(r.status,201);publishers.push((await r.json()).id);}
 assert.equal(new Set(publishers).size,2,'one discovery page cannot mix IDs from different publishers');
 const channels=[];
 for(const channel of ['channel-one','channel-two']){const r=await request('/api/vacancy-candidates','POST',{titleRaw:'Equal message IDs',externalJobId:'42',sourceId:'telegram',sourceUrl:'https://t.me/'+channel,vacancyUrl:'https://t.me/'+channel+'/42'});assert.equal(r.status,201);channels.push((await r.json()).id);}
 assert.equal(new Set(channels).size,2,'channel-local IDs stay scoped to concrete sources');
 const before=sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n;
 const a=await (await request('/api/vacancy-candidates','POST',{titleRaw:'Collision A',vacancyUrl:'https://jobs.example/a',sourceNamespace:'collision',externalJobId:'A'})).json();
 const b=await (await request('/api/vacancy-candidates','POST',{titleRaw:'Collision B',vacancyUrl:'https://jobs.example/b',sourceNamespace:'collision',externalJobId:'B'})).json();
 assert.equal((await request('/api/vacancy-candidates','POST',{titleRaw:'Do not merge',vacancyUrl:'https://jobs.example/b',sourceNamespace:'collision',externalJobId:'A'})).status,409);
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,before+2);
 const list=await (await request('/api/vacancy-candidates?pageSize=2&page=2')).json();assert.equal(list.candidates.length,2);assert.equal(list.total,before+2);
 const filtered=await (await request('/api/vacancy-candidates?companyId='+companyId+'&sourceId=companySite&status=pending_classification')).json();assert.equal(filtered.total,1);assert.equal(filtered.candidates[0].id,id);
 const state=await (await request('/api/vacancy-candidates/'+id)).json();assert.equal(state.currentResults.length,2);assert.equal(state.needsClassification,true);assert.equal(state.history.total,0);
 assert.equal((await request('/api/vacancy-candidates?pageSize=101')).status,400);assert.equal((await request('/api/vacancy-candidates?status=unknown')).status,400);assert.equal((await request('/api/vacancy-candidates/999999')).status,404);
 sqlite.exec('UPDATE vacancy_role_classification SET parsing_enabled=0');assert.equal((await (await request('/api/vacancy-candidates/'+id)).json()).candidate.processingStatus,'no_active_roles');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0,'primary API never creates main jobs');
 sqlite.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').run(company);
 sqlite.prepare('INSERT INTO company_vacancies(id,company_name,title,url) VALUES(90001,?,?,?)').run(company,'Existing main job','https://main.example/job');
 assert.equal((await request('/api/vacancy-candidates/'+id+'/link','PUT',{mainVacancyId:90001})).status,200);
 assert.equal((await (await request('/api/vacancy-candidates/'+id)).json()).candidate.processingStatus,'imported');
 assert.equal((await request('/api/vacancy-candidates/'+id+'/link','PUT',{mainVacancyId:999999})).status,409);
 const unchanged=sqlite.prepare('SELECT title FROM company_vacancies WHERE id=90001').get().title;assert.equal(unchanged,'Existing main job');
 console.log('Candidate API: ACL/CSRF/input bounds/raw title, canonical/external aliases, atomic parallel idempotency, conflicts, scoped IDs, paging/filters, live status, no main creation and existing-main link passed');
}finally{sqlite.close();}
