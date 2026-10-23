import {hhInput} from '../server/integrations/apify.js';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();
const sqlite=new DatabaseSync(':memory:');for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();const db={prepare(sql){let values=[];return {bind(...v){values=v;return this},async first(){return sqlite.prepare(sql).get(...values)??null},async all(){return {results:sqlite.prepare(sql).all(...values)}},async run(){const stmt=sqlite.prepare(sql);return stmt.columns().length?{results:stmt.all(...values)}:{meta:stmt.run(...values)}}}},batch(statements){const p=queue.then(async()=>{sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}});queue=p.catch(()=>{});return p}};
const api=await import('../.tests/backend.mjs'),env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'apify-test-secret'},base='https://apify.test';
const login=async name=>(await api.default.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env)).headers.get('set-cookie').split(';')[0];
const admin=await login('Eugene'),other=await login('Nastya');
const request=(path,method='GET',body,cookie=admin,origin=base)=>api.default.fetch(new Request(base+path,{method,headers:{cookie,origin,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);
const free='apify_api_SYNTHETIC_FREE_12345',starter='apify_api_SYNTHETIC_STARTER_12345';let calls=[],used=0,failStart=false,startCount=0,runStatus='RUNNING';const original=globalThis.fetch;const cycleStart=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth(),1)).toISOString(),cycleEnd=new Date(Date.UTC(new Date().getUTCFullYear(),new Date().getUTCMonth()+1,1)).toISOString();
globalThis.fetch=async(url,options)=>{assert.ok(url.startsWith('https://api.apify.com/v2/'));assert.ok(!url.includes('apify_api_'));assert.equal(options.redirect,'manual');calls.push(url);const paid=options.headers.authorization==='Bearer '+starter;
 if(url.endsWith('/users/me'))return Response.json({data:{id:paid?'paidAccount':'freeAccount',plan:{id:paid?'STARTER':'FREE',isEnabled:true,monthlyBasePriceUsd:paid?19:0,monthlyUsageCreditsUsd:paid?19:5},proxy:{password:'DO_NOT_SAVE'},email:'DO_NOT_SAVE'}});
 if(url.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:cycleStart,endAt:cycleEnd},limits:{maxMonthlyUsageUsd:paid?19:5},current:{monthlyUsageUsd:used}}});
 if(options.method==='POST'){startCount++;const u=new URL(url);assert.equal(u.searchParams.get('maxTotalChargeUsd'),'0.25');assert.equal(u.searchParams.get('waitForFinish'),'0');assert.equal(u.searchParams.get('restartOnError'),'false');assert.equal(JSON.parse(options.body).proxyConfiguration.useApifyProxy,true);if(failStart)throw new Error(free);return Response.json({data:{id:'run'+startCount,defaultDatasetId:'data'+startCount,status:'RUNNING'}});}
 if(url.includes('/actor-runs/'))return Response.json({data:{id:'run'+startCount,defaultDatasetId:'data'+startCount,status:runStatus,usageTotalUsd:0.02}});
 if(url.includes('/datasets/'))return Response.json([]);throw new Error('Unexpected URL');};
try{
 assert.equal((await request('/api/apify/settings','GET',undefined,other)).status,403);
 let s=await (await request('/api/apify/settings')).json();assert.equal(s.enabled,false);assert.equal(s.paidAllowed,false);
 assert.equal((await request('/api/apify/key','PUT',{apiKey:free,profile:'free',revision:s.revision},admin,'https://evil.test')).status,403);
 s=await (await request('/api/apify/key','PUT',{apiKey:free,profile:'free',revision:s.revision})).json();assert.equal(s.profiles.free.configured,true);assert.ok(!JSON.stringify(s).includes(free));assert.ok(!sqlite.prepare('SELECT free_key_encrypted FROM apify_settings').get().free_key_encrypted.includes(free));
 assert.equal((await request('/api/apify/key','PUT',{apiKey:free,profile:'free',revision:0})).status,409);
 const checked=await (await request('/api/apify/check','POST',{profile:'free'})).json();assert.equal(checked.budget.limitUsd,5);assert.equal(checked.budget.remainingUsd,5);assert.ok(!JSON.stringify(checked).includes('DO_NOT_SAVE'));assert.equal(startCount,0);
 s=await (await request('/api/apify/key','PUT',{apiKey:starter,profile:'starter',revision:s.revision})).json();assert.equal((await request('/api/apify/settings','PUT',{enabled:true,paidAllowed:true,revision:s.revision})).status,400);
 s=await (await request('/api/apify/settings','PUT',{enabled:true,paidAllowed:false,revision:s.revision})).json();assert.equal(s.enabled,true);
 const transport=await moduleFor('server/integrations/apify.js'),runs=await moduleFor('server/services/apify-runs.js');
 for(const url of ['https://hh.ru.evil.test/employer/1','http://hh.ru/employer/1','https://hh.kz/employer/1','https://127.0.0.1/employer/1','https://hh.ru/search/vacancy?text=test','https://evil.test/vacancy/1'])assert.throws(()=>transport.hhInput(url));
 const input=transport.hhInput('https://sber.hh.ru/employer/3529',{page:3});assert.equal(new URL(input.startUrl).searchParams.get('employer_id'),'3529');assert.equal(new URL(input.startUrl).searchParams.get('page'),'3');assert.equal(input.results_wanted,100);assert.equal(input.max_pages,1);
 let result=await runs.executeApifyRun(env,{operationKey:'one',input});assert.equal(result.pending,true);assert.equal(startCount,1);result=await runs.executeApifyRun(env,{operationKey:'one',input});assert.equal(startCount,1,'poll is never another start');
 runStatus='SUCCEEDED';result=await runs.executeApifyRun(env,{operationKey:'one',input});assert.equal(result.pending,false);assert.equal(result.row.charged_usd,0.02);
 used=5;await assert.rejects(runs.executeApifyRun(env,{operationKey:'budget',input}),e=>e.code==='apify_budget');assert.equal(startCount,1,'no paid fallback without opt in');
 s=await (await request('/api/apify/settings','PUT',{enabled:true,paidAllowed:true,confirmed:true,revision:s.revision})).json();used=4.8;result=await runs.executeApifyRun(env,{operationKey:'paid',input});assert.equal(result.row.profile,'starter');assert.equal(startCount,2);
 runStatus='SUCCEEDED';await runs.executeApifyRun(env,{operationKey:'paid',input});used=0;failStart=true;await assert.rejects(runs.executeApifyRun(env,{operationKey:'lost',input}));assert.equal(startCount,3);await assert.rejects(runs.executeApifyRun(env,{operationKey:'lost',input}),e=>e.code==='apify_uncertain');assert.equal(startCount,3);
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.ok(backup.storage.tables.some(t=>t.name==='apify_settings'));assert.ok(!backup.storage.tables.some(t=>t.name==='apify_runs'));assert.ok(!JSON.stringify(backup).includes(free));await api.restoreBackup(db,backup);const restored=sqlite.prepare('SELECT * FROM apify_settings').get();assert.equal(restored.enabled,0);assert.equal(restored.paid_allowed,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,3,'reservations survive restore');
 console.log('Apify: ACL/Origin, encrypted profiles/CAS, no-secret billing check, real Free/Starter budgets and opt in, HH URL scope, bounded async start/poll, no replay after lost ACK, backup36 and preserved ledger passed.');
}finally{globalThis.fetch=original;sqlite.close();}

assert.equal(hhInput('https://hh.ru/employer/1').api_only,true,'list collection avoids per-card enrichment');
assert.equal(hhInput('https://hh.ru/vacancy/1',{detail:true}).api_only,false,'detail retains enrichment');
