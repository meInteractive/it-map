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
const api=await import('../.tests/backend.mjs?firecrawl-settings'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);

const secret='fc-SYNTHETIC-FIRECRAWL-KEY';
try{
 assert.equal((await request('/api/firecrawl/settings','GET',undefined,user)).status,403);
 let state=await (await request('/api/firecrawl/settings')).json();assert.equal(state.favoritesOnly,true);assert.equal(state.enabled,false);assert.equal(state.configured,false);assert.equal(state.maxPages,3);assert.equal(state.concurrency,1);assert.equal(state.proxy,'basic');
 let response=await request('/api/firecrawl/key','PUT',{apiKey:secret,revision:state.revision});assert.equal(response.status,200,await response.clone().text());state=await response.json();assert.equal(state.configured,true);assert.equal(state.enabled,false);assert.ok(!JSON.stringify(state).includes(secret));
 const row=sqlite.prepare('SELECT * FROM firecrawl_settings').get();assert.ok(!row.key_encrypted.includes(secret));assert.equal(await api.decryptGoogleToken(env.SESSION_SECRET+':firecrawl:v1',row.key_encrypted),secret);
 assert.equal((await request('/api/firecrawl/key','PUT',{apiKey:secret,revision:0})).status,409);
 const config={enabled:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:state.revision};
 assert.equal((await request('/api/firecrawl/settings','PUT',{...config,concurrency:2})).status,400);
 assert.equal((await request('/api/firecrawl/settings','PUT',{...config,maxPages:11})).status,400);
 assert.equal((await request('/api/firecrawl/settings','PUT',{...config,proxy:'auto'})).status,400);
 assert.equal((await request('/api/firecrawl/settings','PUT',{...config,proxy:'enhanced'})).status,400);
 state=await (await request('/api/firecrawl/settings','PUT',config)).json();assert.equal(state.enabled,true);
 // Empty own favorites must not fall back to somebody else's favorites.
 const sources=await moduleFor('server/repositories/vacancy-source-parsing.js');
 const all=await sources.readVacancyParsingSources(db),selected=all.find(s=>s.supported),other=all.find(s=>s.supported&&s.companyId!==selected.companyId);
 assert.ok(selected&&other);
 for(const source of [selected,other])sqlite.prepare("INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)").run(source.companyName);
 sqlite.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Nastya',?,1)").run(other.companyName);
 assert.deepEqual((await (await request('/api/firecrawl/sources')).json()).sources,[]);
 sqlite.prepare("INSERT OR REPLACE INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(selected.companyName);
 const own=(await (await request('/api/firecrawl/sources')).json()).sources;
 assert.ok(own.length&&own.every(s=>s.companyId===selected.companyId));
 assert.deepEqual((await (await request('/api/firecrawl/sources?companyId='+other.companyId)).json()).sources,[]);
 const foreign=await sources.readVacancyParsingSources(db,{favoritesOnly:true,userName:'Nastya'});assert.ok(foreign.length&&foreign.every(s=>s.companyId===other.companyId));
 assert.equal((await request('/api/firecrawl/settings','PUT',{...config,revision:state.revision,favoritesOnly:'false'})).status,400);
 let deniedCalls=0;const originalScopeFetch=globalThis.fetch;globalThis.fetch=async()=>{deniedCalls++;throw new Error('No outbound for nonfavorite')};
 try{const denied=await request('/api/firecrawl/test','POST',{companyId:other.companyId,sourceId:other.sourceId});assert.equal(denied.status,403);assert.equal((await denied.json()).code,'favorites_only');assert.equal(deniedCalls,0);}finally{globalThis.fetch=originalScopeFetch;}
 state=await (await request('/api/firecrawl/settings','PUT',{...config,revision:state.revision,favoritesOnly:false})).json();assert.equal(state.favoritesOnly,false);
 assert.ok((await (await request('/api/firecrawl/sources')).json()).sources.some(s=>s.companyId===other.companyId));
 state=await (await request('/api/firecrawl/settings','PUT',{...config,revision:state.revision})).json();assert.equal(state.favoritesOnly,false,'old client preserves explicit off');
 const originalFetch=globalThis.fetch;let count=0;
 globalThis.fetch=async(url,options)=>{count++;assert.equal(url,'https://api.firecrawl.dev/v2/team/credit-usage');assert.equal(options.headers.authorization,'Bearer '+secret);return Response.json({success:true,data:{remainingCredits:42,planCredits:1000,billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'}});};
 try{response=await request('/api/firecrawl/check','POST',{});assert.equal(response.status,200,await response.clone().text());const checked=await response.json();assert.equal(checked.remainingCredits,42);assert.equal(checked.state.connectionStatus,'connected');assert.equal(count,1);
 sqlite.prepare('INSERT INTO firecrawl_request_lease VALUES(1,?,?,?)').run(sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation,crypto.randomUUID(),Date.now()+60000);
 assert.equal((await request('/api/firecrawl/check','POST',{})).status,409);assert.equal(count,1);sqlite.exec('DELETE FROM firecrawl_request_lease');
 globalThis.fetch=async()=>Response.json({error:secret},{status:401});response=await request('/api/firecrawl/check','POST',{});assert.equal(response.status,400);assert.ok(!(await response.text()).includes(secret));assert.equal(sqlite.prepare('SELECT connection_status FROM firecrawl_settings').get().connection_status,'error');
 }finally{globalThis.fetch=originalFetch;}
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.ok(!JSON.stringify(backup).includes(secret));assert.ok(backup.storage.tables.some(t=>t.name==='firecrawl_settings'));assert.ok(!backup.storage.tables.some(t=>t.name==='firecrawl_request_lease'));
 await api.restoreBackup(db,backup);assert.equal((await api.firecrawlSettings(db)).favorites_only,0,'current backup keeps off');
 const old33=structuredClone(backup);old33.version=33;old33.storage.tables=old33.storage.tables.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));old33.manifest=old33.manifest.filter(t=>!['parser_error_daily','parser_candidate_intakes'].includes(t.name));old33.storage.tables=old33.storage.tables.filter(t=>t.name!=='apify_settings');old33.manifest=old33.manifest.filter(t=>t.name!=='apify_settings');const oldFc=old33.storage.tables.find(t=>t.name==='firecrawl_settings');oldFc.columns=oldFc.columns.filter(c=>c.name!=='favorites_only');for(const r of oldFc.rows)delete r.favorites_only;old33.manifest.find(t=>t.name==='firecrawl_settings').columns=oldFc.columns.map(c=>c.name);for(const t of old33.storage.tables){const added=t.name==='firecrawl_settings'?['scheduler_json']:t.name==='vacancy_source_parsing'?['auto_enabled','interval_days','next_run_at','last_attempt_at','last_result']:[];t.columns=t.columns.filter(c=>!added.includes(c.name));for(const row of t.rows)for(const name of added)delete row[name];const m=old33.manifest.find(m=>m.name===t.name);if(m)m.columns=m.columns.filter(c=>!added.includes(c));}const oldRole=old33.storage.tables.find(t=>t.name==='vacancy_role_settings');oldRole.columns=oldRole.columns.filter(c=>c.name!=='openrouter_model');for(const row of oldRole.rows)delete row.openrouter_model;old33.manifest.find(t=>t.name==='vacancy_role_settings').columns=oldRole.columns.map(c=>c.name);for(const t of old33.storage.tables.filter(t=>['vacancy_candidates','vacancy_source_parsing'].includes(t.name))){t.columns=t.columns.filter(c=>c.name!=='agency_id');for(const row of t.rows)delete row.agency_id;old33.manifest.find(m=>m.name===t.name).columns=t.columns.map(c=>c.name);}old33.integrity.digest=await api.backupDigest(old33);
 await api.restoreBackup(db,old33);assert.equal((await api.firecrawlSettings(db)).favorites_only,1,'old backup enables filter');
 sqlite.exec('ALTER TABLE firecrawl_settings DROP COLUMN favorites_only');const upgrade=await moduleFor('server/repositories/firecrawl-policy-schema.js');await Promise.all([upgrade.ensureFirecrawlPolicySchema(db),upgrade.ensureFirecrawlPolicySchema(db)]);assert.equal((await api.firecrawlSettings(db)).favorites_only,1,'existing installation upgrades with default on');
 console.log('Firecrawl favorites defaults/own scope/empty/no outbound/CAS/persist/backup34+33/concurrent upgrade passed; settings: admin ACL, encrypted key/no leaks, CAS, limits, proxy opt-in, cheap connection check, global concurrency and backup30 passed');
}finally{sqlite.close();}
