import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

for(const dir of ['server/services','server/repositories','server/routes'])for(const name of fs.readdirSync(dir).filter(n=>n.endsWith('.js'))){const body=fs.readFileSync(dir+'/'+name,'utf8');if(body.includes('firecrawl-transport.js'))assert.equal(dir+'/'+name,'server/services/firecrawl-usage-policy.js','raw transport only imported by central policy');assert.ok(!body.includes('https://api.firecrawl.dev/'),'no direct Firecrawl HTTP bypass');}
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



const originalFetch=globalThis.fetch;const period={billingPeriodStart:'2026-10-01T00:00:00Z',billingPeriodEnd:'2026-11-01T00:00:00Z'};
let balance=1000,plan=1000,paid=0,metadata=0,mode='success',hold=false,release,lastAuth;
globalThis.fetch=async(url,options)=>{
 if(url.endsWith('credit-usage')){metadata++;return Response.json({success:true,data:{remainingCredits:balance,planCredits:plan,...(mode==='no-period'?{}:period)}});}
 assert.equal(url,'https://api.firecrawl.dev/v2/scrape');lastAuth=options.headers.authorization;paid++;const body=JSON.parse(options.body);assert.ok(!body.formats.includes('json'));assert.equal(body.proxy,'basic');if(hold)await new Promise(r=>release=r);if(mode==='network')throw new Error('Uncertain request');if(mode==='rate')return Response.json({success:false},{status:429});return Response.json({success:true,data:{markdown:'[Developer](/jobs/42)',links:['/jobs/42'],metadata:{statusCode:200,url:body.url,...(mode==='unknown'?{}:{creditsUsed:1})}}});
};
const options={hosts:['careers.company.org'],timeoutMs:45000,proxy:'basic',budget:{remaining:1}},url='https://careers.company.org/jobs';
const session=()=>api.createFirecrawlUsageSession(env);
const scrape=async()=>api.firecrawlScrape(await session(),url,{...options,budget:{remaining:1}});
const clean=()=>sqlite.exec('DELETE FROM firecrawl_usage_events; DELETE FROM firecrawl_usage_periods');
try{
 let state=await (await request('/api/firecrawl/settings')).json();state=await (await request('/api/firecrawl/key','PUT',{apiKey:'fc-SYNTHETIC-FREE-KEY',revision:state.revision})).json();state=await (await request('/api/firecrawl/settings','PUT',{enabled:true,maxPages:3,concurrency:1,timeoutMs:45000,proxy:'basic',enhancedAllowed:false,revision:state.revision})).json();
 await assert.rejects(()=>api.firecrawlScrape('RAW-KEY',url,options),e=>e.code==='policy');assert.equal(paid,0);
 await assert.rejects(async()=>api.firecrawlScrape(await session(),url,{...options,proxy:'enhanced',enhancedAllowed:true}),e=>e.code==='policy');assert.equal(paid,0);
 mode='no-period';await assert.rejects(scrape,e=>e.code==='usage_unavailable');assert.equal(paid,0);
 mode='success';balance=1001;await assert.rejects(scrape,e=>e.code==='usage_unavailable');assert.equal(paid,0);
 balance=3;await assert.rejects(scrape,e=>e.code==='free_exhausted');assert.equal(paid,0);clean();balance=4;await scrape();const delayedBefore=paid;await assert.rejects(scrape,e=>e.code==='free_exhausted');assert.equal(paid,delayedBefore,'known local charge is not silently freed when fresh API balance lags');clean();balance=1000;
 const raceBefore=paid;hold=true;const first=scrape();for(let i=0;i<1000&&!release;i++)await new Promise(r=>setTimeout(r,1));assert.ok(release);assert.equal(sqlite.prepare("SELECT count(*) n FROM firecrawl_usage_events WHERE outcome='reserved'").get().n,1);await assert.rejects(scrape,e=>e.code==='busy');assert.equal(paid,raceBefore+1);hold=false;release();await first;assert.equal(sqlite.prepare('SELECT charged_credits FROM firecrawl_usage_events').get().charged_credits,1);
 clean();for(let i=0;i<10;i++)await scrape();const before=paid;await assert.rejects(scrape,e=>e.code==='rate_limit');assert.equal(paid,before);sqlite.prepare('UPDATE firecrawl_usage_events SET started_at=?').run(Date.now()-61000);await scrape();assert.equal(paid,before+1);
 clean();mode='unknown';await scrape();let row=sqlite.prepare('SELECT * FROM firecrawl_usage_events').get();assert.equal(row.charged_credits,null);assert.equal(row.outcome,'uncertain');assert.equal(row.reserved_credits,4);
 clean();mode='network';await assert.rejects(scrape,e=>e.code==='network');row=sqlite.prepare('SELECT * FROM firecrawl_usage_events').get();assert.ok(row.active_until>Date.now());assert.equal(row.charged_credits,null);const networkPaid=paid;await assert.rejects(scrape,e=>e.code==='busy');assert.equal(paid,networkPaid);clean();mode='success';
 state=await (await request('/api/firecrawl/settings')).json();state=await (await request('/api/firecrawl/key','PUT',{mode:'hobby',apiKey:'fc-SYNTHETIC-HOBBY-KEY',revision:state.revision})).json();plan=5000;balance=5000;state=await (await request('/api/firecrawl/mode','PUT',{mode:'hobby',confirmed:true,revision:state.revision})).json();assert.equal(state.mode,'hobby');
 const hobbyBefore=paid;balance=3;await assert.rejects(scrape,e=>e.code==='hobby_exhausted');assert.equal(paid,hobbyBefore);clean();balance=5000;for(let i=0;i<100;i++)await scrape();const rateBefore=paid;await assert.rejects(scrape,e=>e.code==='rate_limit');assert.equal(paid,rateBefore);clean();
 const old=await session();hold=true;release=undefined;const running=api.firecrawlScrape(old,url,{...options,budget:{remaining:1}});for(let i=0;i<1000&&!release;i++)await new Promise(r=>setTimeout(r,1));assert.ok(release);assert.equal(lastAuth,'Bearer fc-SYNTHETIC-HOBBY-KEY');state=await (await request('/api/firecrawl/mode','PUT',{mode:'free',revision:state.revision})).json();hold=false;release();const completed=await running;assert.equal(completed.profile,'hobby');assert.equal(lastAuth,'Bearer fc-SYNTHETIC-HOBBY-KEY');const beforeOld=paid;await assert.rejects(()=>api.firecrawlScrape(old,url,options),e=>e.code==='profile_changed');assert.equal(paid,beforeOld);
 plan=1000;balance=1000;await scrape();const rowsBefore=sqlite.prepare('SELECT * FROM firecrawl_usage_events').all();const backup=await api.createBackup(db);assert.ok(!backup.storage.tables.some(t=>t.name==='firecrawl_usage_events'));await api.restoreBackup(db,backup);assert.deepEqual(sqlite.prepare('SELECT * FROM firecrawl_usage_events').all(),rowsBefore,'restore never rolls back usage history');
 assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);
 console.log('Firecrawl policy: no raw-key bypass, forbidden options, period/bonus fail-closed, pre-request cap, atomic reservations/concurrency, minute limit, unknown/timeout exposure, no fallback, mode switch and restore budget persistence passed');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
