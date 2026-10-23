import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';

// Exercise the real HTTP handlers against SQLite, including fresh Worker instances.
const sqlite = new DatabaseSync(':memory:');
sqlite.exec('PRAGMA foreign_keys = ON');
const db = {
  prepare(sql) {
    let values = [];
    return {
      bind(...args) { values = args; return this; },
      async all() { return {results:sqlite.prepare(sql).all(...values)}; },
      async first() { return sqlite.prepare(sql).get(...values) ?? null; },
      async run() {
        const prepared = sqlite.prepare(sql);
        if (prepared.columns().length) return {success:true, results:prepared.all(...values)};
        const result = prepared.run(...values);
        return {success:true,meta:{changes:Number(result.changes),last_row_id:Number(result.lastInsertRowid)}};
      }
    };
  },
  async batch(statements) {
    sqlite.exec('BEGIN');
    try {
      const results = [];
      for (const statement of statements) results.push(await statement.run());
      sqlite.exec('COMMIT');
      return results;
    } catch (error) { sqlite.exec('ROLLBACK'); throw error; }
  }
};
const env = {DB:db,SITE_PASSWORD:'test-only',NASTYA_PASSWORD:'test-only',SESSION_SECRET:'test-only-secret'};
const worker = (await import(new URL('../.tests/backend.mjs?openrouter-test', import.meta.url))).default;
import fs from 'node:fs';
import vm from 'node:vm';
const functions=await import('../.tests/backend.mjs');
const model=(id,context=262144,extra={})=>({id,name:id,canonical_slug:id.replace(':free',''),context_length:context,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:'0',completion:0},...extra});
const b=(id,i,a=1,c=1)=>({source:'artificial-analysis',model_permaslug:id,intelligence_index:i,agentic_index:a,coding_index:c});
let models=[model('test/one:free'),model('test/two'),model('test/unknown',1048576),model('test/tiny',262143),model('test/paid:free',262144,{pricing:{prompt:0,completion:0.001}}),model('test/fee',262144,{pricing:{prompt:0,completion:0,request:0.1}}),model('test/missingprice',262144,{pricing:{prompt:null,completion:''}}),model('test/no-reason',262144,{supported_parameters:[]}),model('test/no-text',262144,{architecture:{output_modalities:['image']}}),model('openrouter/free')];
let benchmarks=[b('test/one',90),b('test/two',80),b('test/unknown',null)];
assert.deepEqual(functions.orRankModels(models,benchmarks,'date').map(m=>m.id),['test/one:free','test/two','test/unknown']);
assert.equal(functions.orRankModels(models,benchmarks,'date')[2].intelligence_index,null);
const ties=[model('t/a',262144),model('t/b',524288),model('t/c',524288),model('t/d',1048576)];
assert.deepEqual(functions.orRankModels(ties,[b('t/a',10,3,2),b('t/b',10,3,2),b('t/c',10,4,1),b('t/d',10,3,3)],'date').map(m=>m.id),['t/c','t/d','t/b','t/a']);
const originalFetch=globalThis.fetch;let mode='ok',calls=[],failSync=false,benchMissing=false;
globalThis.fetch=async(url,options)=>{
 assert.ok(url.startsWith('https://openrouter.ai/api/v1/'));assert.ok(options.headers.Authorization.startsWith('Bearer sk-or-'));
 if(mode==='invalid')return Response.json({error:{message:'SECRET upstream body'}},{status:401});
 if(url.includes('/models?')){assert.ok(url.includes('context=262144'));if(failSync)return Response.json({error:{message:'SECRET'}},{status:500});return Response.json({data:models});}
 if(url.includes('/benchmarks?'))return Response.json({data:benchMissing?[]:benchmarks});
 if(url.endsWith('/key'))return Response.json({data:{label:'test'}});
 const body=JSON.parse(options.body);calls.push(body);assert.equal(body.provider.max_price.prompt,0);assert.equal(body.provider.max_price.completion,0);assert.equal(body.provider.max_price.request,0);assert.equal(body.provider.require_parameters,true);assert.ok(body.reasoning.effort==='high'||body.reasoning.enabled===true);
 assert.equal(typeof body.model,'string');assert.notEqual(body.model,'openrouter/free');assert.ok(!('models' in body),'Each attempt must address only one model');
 if(mode==='down')return Response.json({error:{message:'SECRET'}},{status:429});
 if((mode==='adapt'||mode==='adapt-fail')&&body.reasoning.effort)return Response.json({error:{message:'Unsupported reasoning effort high'}},{status:400});
 const first=body.model==='test/one:free',second=body.model==='test/two';
 if(mode==='transport'&&first)throw Error('SECRET network');
 if(mode==='fallback'&&first)return Response.json({error:{message:'Unavailable'}},{status:503});
 if(mode==='two-fail'&&(first||second))return Response.json({error:{message:'Unavailable'}},{status:first?429:503});
 if(mode==='adapt-fail'&&first)return Response.json({error:{message:'Unavailable'}},{status:503});
 if(mode.startsWith('status-')&&first)return Response.json({error:{message:'Unavailable'}},{status:Number(mode.slice(7))});
 if(mode==='bad-json'&&first)return new Response('not json');
 if(mode==='wrong-model'&&first)return Response.json({model:'test/unknown',choices:[{message:{content:'Answer from an unrequested model'}}]});
 if(mode==='empty'&&first)return Response.json({model:'test/one',choices:[{message:{content:'   '}}]});
 if(mode==='refusal'&&first)return Response.json({model:'test/one',choices:[{message:{content:null,refusal:'Refused'},finish_reason:'content_filter'}]});
 return Response.json({model:body.model.replace(':free',''),choices:[{message:{content:'PRIVATE_RESPONSE',reasoning:'PRIVATE_REASONING'}}],usage:{total_tokens:1}});
};
async function login(name){const r=await worker.fetch(new Request('https://test.example/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test-only'})}),env);return r.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
async function req(path='',method='GET',body,cookie=admin,origin){return worker.fetch(new Request('https://test.example/api/openrouter'+path,{method,headers:{...(cookie?{cookie}:{}),'content-type':'application/json',...(origin?{origin}:{})},body:body?JSON.stringify(body):undefined}),env);}
async function ok(path='',method='GET',body){const r=await req(path,method,body);const d=await r.json();assert.equal(r.status,200,JSON.stringify(d));return d;}
try{
 for(const path of ['', '/key','/check','/refresh','/test','/logs']){assert.equal((await req(path,'GET',null,null)).status,401);assert.equal((await req(path,'GET',null,user)).status,403);}
 assert.equal((await ok()).configured,false);assert.equal((await req('/test','POST')).status,400);
 assert.equal((await req('/key','PUT',{apiKey:'bad'})).status,400);
 const key='FAKE_TEST_TOKEN_81efd70a208d';
 assert.equal((await req('/key','PUT',{apiKey:key},admin,'https://evil.test')).status,403);
 let state=await ok('/key','PUT',{apiKey:key});assert.equal(state.models.length,3);assert.equal(state.primary_model_id,'test/one:free');assert.equal(state.key_mask,'••••7890');assert.ok(!JSON.stringify(state).includes(key));
 const stored=sqlite.prepare('SELECT * FROM openrouter_settings').get();assert.ok(stored.key_encrypted&&!stored.key_encrypted.includes(key));assert.equal(stored.revision,1);
 assert.equal((await ok('/check','POST')).connection_status,'connected');
 const requested=()=>calls.map(c=>c.model);
 mode='fallback';calls=[];let result=await ok('/test','POST');assert.equal(result.model,'test/two');assert.equal(result.fallback_used,true);assert.deepEqual(requested(),['test/one:free','test/two']);
 // Each new request restarts from #1, including after a successful fallback.
 mode='ok';calls=[];result=await ok('/test','POST');assert.deepEqual(requested(),['test/one:free']);assert.equal(result.fallback_used,false);assert.equal(result.state.last_used_model,'test/one');
 mode='adapt';calls=[];await ok('/test','POST');assert.deepEqual(requested(),['test/one:free','test/one:free']);assert.equal(calls[1].reasoning.enabled,true);
 mode='adapt-fail';calls=[];result=await ok('/test','POST');assert.deepEqual(requested(),['test/one:free','test/one:free','test/two','test/two']);assert.equal(result.model,'test/two');
 for(const failure of ['transport','status-429','status-404','status-403','status-502','status-503','bad-json','wrong-model','empty','refusal']){
  mode=failure;calls=[];result=await ok('/test','POST');assert.deepEqual(requested(),['test/one:free','test/two'],failure+' must advance to #2, then stop');assert.equal(result.model,'test/two');assert.equal(result.fallback_used,true);
 }
 mode='two-fail';calls=[];result=await ok('/test','POST');assert.deepEqual(requested(),['test/one:free','test/two','test/unknown']);assert.equal(result.model,'test/unknown');assert.deepEqual(result.attempted_models,requested());
 mode='down';calls=[];let r=await req('/test','POST');assert.equal(r.status,503);assert.deepEqual(requested(),['test/one:free','test/two','test/unknown']);assert.match((await r.json()).error,/Все подходящие модели/);
 mode='ok';calls=[];await ok('/test','POST');assert.deepEqual(requested(),['test/one:free'],'Failure must not change the starting model');
 mode='invalid';r=await req('/check','POST');assert.equal(r.status,400);assert.ok(!(await r.text()).includes('SECRET'));
 mode='ok';await ok('/check','POST');failSync=true;r=await req('/refresh','POST');assert.equal(r.status,502);state=await ok();assert.equal(state.models.length,3);assert.ok(state.warning);
 failSync=false;benchmarks=[b('test/two',99),b('test/one',80)];state=await ok('/refresh','POST');assert.equal(state.primary_model_id,'test/two');assert.equal(state.warning,'');
 calls=[];await ok('/test','POST');assert.deepEqual(requested(),['test/two'],'New rankings determine the next request');
 benchMissing=true;state=await ok('/refresh','POST');assert.ok(state.models.every(m=>m.intelligence_index===null));benchMissing=false;
 sqlite.prepare("UPDATE openrouter_settings SET models_synced_at='2020-01-01'").run();state=await ok();assert.equal(state.primary_model_id,'test/two');assert.ok(state.models_synced_at.startsWith('2026'));
 models=[];state=await ok('/refresh','POST');assert.equal(state.models.length,0);assert.match(state.empty_message,/Нет бесплатных/);assert.equal((await req('/test','POST')).status,503);
 const logs=await ok('/logs');assert.ok(logs.logs.some(l=>l.fallback_used===1));assert.ok(logs.logs.some(l=>l.success===0));assert.ok(!/PRIVATE|SECRET|sk-or-/.test(JSON.stringify(logs)));
 const backup=await worker.fetch(new Request('https://test.example/api/backup',{headers:{cookie:admin}}),env);const backupText=await backup.text();assert.equal(backup.status,200);assert.ok(!backupText.includes(key));assert.ok(backupText.includes(stored.key_encrypted));
 const {default:Settings}=await moduleFor('frontend/features/settings/Settings.tsx');assert.match(render(Settings,{user:sessionUser(true),tab:'ai'}),/Нейросети/);assert.ok(!render(Settings,{user:sessionUser(false),tab:'ai'}).includes('Нейросети'));
 console.log('OpenRouter verified: strict free/reasoning/context filters, benchmark joins and ranking, dynamic rotation, admin-only routes, encrypted key and backup, strict #1→#2→#3 fallback, restart, response identity and adaptive reasoning, cache failure/expiry, safe logs and empty models.');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
