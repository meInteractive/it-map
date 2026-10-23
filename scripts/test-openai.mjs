import {prepareBackend} from './test-support.mjs';
import assert from 'node:assert/strict';
import {openaiPricing} from '../server/integrations/openai-pricing.js';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
await prepareBackend();const api=await import('../.tests/backend.mjs'),worker=api.default;
const sqlite=new DatabaseSync(':memory:');
for(const file of fs.readdirSync('drizzle').filter(n=>n.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(){return sqlite.prepare(sql).get(...args)||null;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const q=sqlite.prepare(sql);return q.columns().length?{results:q.all(...args)}:{meta:q.run(...args)};}};},async batch(statements){sqlite.exec('BEGIN');try{const result=[];for(const s of statements)result.push(await s.run());sqlite.exec('COMMIT');return result;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
const env={DB:db,OPENAI_API_KEY:'sk-SYNTHETIC-OPENAI-KEY',SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',DEMO_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'};
const base='https://openai-test.example',cookies={};
for(const name of ['Eugene','Nastya','Demo']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'})}),env);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
const req=(name,path,method='GET',payload,origin=base,activeEnv=env)=>worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[name],origin,'content-type':'application/json'},...(payload?{body:JSON.stringify(payload)}:{})}),activeEnv);
const originalFetch=fetch;let available=['gpt-6-luna','gpt-5.6-terra','gpt-6.1-sol','gpt-6-sol','gpt-5.6-sol','gpt-4.1','gpt-image-2','text-embedding-3-small','gpt-realtime','gpt-4o-audio-preview'],mode='normal',calls=[],pending,release,expectedKey=env.OPENAI_API_KEY;
const answer=JSON.stringify({subject:'OpenAI subject',markdown:'OpenAI **letter**'});
globalThis.fetch=async(url,options={})=>{
 assert.ok(String(url).startsWith('https://api.openai.com/v1/'),'No cross-provider fallback');
 assert.equal(options.headers.Authorization,'Bearer '+expectedKey);
 if(url.endsWith('/models'))return Response.json({data:available.map(id=>({id}))});
 const body=JSON.parse(options.body);calls.push(body);assert.equal(options.signal.aborted,false);
 if(mode==='held'){pending=true;await new Promise(r=>release=r);}
 if(mode==='rate')return Response.json({error:{message:'secret provider detail '+env.OPENAI_API_KEY}},{status:429});
 if(mode==='network')throw new Error('secret network detail');
 if(mode==='incomplete')return Response.json({model:body.model,status:'incomplete',output:[]});
 if(mode==='bad')return Response.json({model:body.model,status:'completed',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:'not JSON'}]}]});
 return Response.json({model:mode==='mismatch'?'gpt-other':body.model+'-2026-10-01',status:'completed',output:[{type:'reasoning',summary:[]},{type:'message',role:'assistant',content:[{type:'output_text',text:answer}]}]});
};
try{
 for(const [id,input,output] of [['gpt-6-luna',0.1,0.5],['gpt-5.6-terra',2,12],['gpt-6.1-sol',2,10],['gpt-5.6',4,20],['gpt-4.1',2,8],['o1-pro',150,600]]){const price=openaiPricing(id);assert.equal(price.inputUsdPerMillion,input);assert.equal(price.outputUsdPerMillion,output);assert.equal(price.currency,'USD');assert.equal(price.tier,'standard');assert.equal(price.verifiedAt,'2026-10-09');assert.ok(price.source.startsWith('https://developers.openai.com/api/docs/'));}
 for(const id of ['gpt-6.1-luna','gpt-6-luna-pro','gpt-4o-2024-05-13','gpt-6-luna-2026-10-01','ft:gpt-4.1:synthetic','__proto__','constructor'])assert.equal(openaiPricing(id),null,'No guessed tariff for '+id);
 for(const name of ['Nastya','Demo'])for(const [path,method] of [['/api/openai','GET'],['/api/openai/test','POST'],['/api/openai/refresh','POST']])assert.equal((await req(name,path,method)).status,403);
 const publicState=await (await req('Eugene','/api/openai')).json();assert.equal(publicState.defaultModel,'gpt-6-luna');assert.match(publicState.warning,/6.1 Luna/);assert.ok(publicState.models.some(m=>m.id==='gpt-4.1'));assert.ok(!publicState.models.some(m=>/image|audio|embedding|realtime/.test(m.id)));assert.ok(!JSON.stringify(publicState).includes(env.OPENAI_API_KEY));
 const lunaPrice=publicState.models.find(m=>m.id==='gpt-6-luna').pricing;assert.equal(lunaPrice.outputUsdPerMillion,0.5);assert.equal(lunaPrice.inputUsdPerMillion,0.1);assert.equal(calls.length,0,'Catalog pricing needs no generation or billing request');
 assert.equal((await req('Eugene','/api/openai/test','POST',undefined,'https://evil.test')).status,403);
 let r=await req('Eugene','/api/openai','GET',undefined,base,{...env,OPENAI_API_KEY:''});assert.equal((await r.json()).configured,false);
 const secret='FAKE_TEST_TOKEN_7c4e0e463f8b';
 for(const name of ['Nastya','Demo'])assert.equal((await req(name,'/api/openai/key','PUT',{apiKey:secret})).status,403);
 assert.equal((await req('Eugene','/api/openai/key','PUT',{apiKey:secret},'https://evil.test')).status,403);
 assert.equal((await req('Eugene','/api/openai/key','PUT',{apiKey:'FAKE_TEST_TOKEN_94505393fdef'})).status,400);
 assert.equal((await req('Eugene','/api/openai/key','PUT',{apiKey:'x'.repeat(3000)})).status,413);
 assert.equal((await req('Eugene','/api/openai/key','PUT',{apiKey:secret,unexpected:true})).status,400);
 r=await req('Eugene','/api/openai/key','PUT',{apiKey:secret});assert.equal(r.status,200);expectedKey=secret;
 assert.deepEqual(await r.json(),{configured:true,key_mask:'••••7890'});
 const stored=sqlite.prepare('SELECT * FROM openai_settings').get();assert.notEqual(stored.key_encrypted,secret);assert.ok(!JSON.stringify(stored).includes(secret));
 assert.equal(await api.decryptGoogleToken(env.SESSION_SECRET+':openai:v1',stored.key_encrypted),secret);
 const masked=await (await req('Eugene','/api/openai')).json();assert.equal(masked.key_mask,'••••7890');assert.ok(!JSON.stringify(masked).includes(secret));
 const corrupt=await api.openaiPublic({...env,SESSION_SECRET:'wrong-secret'});assert.match(corrupt.warning,/расшифровать/);
 const config={provider:'openai',openaiModel:'gpt-6-luna',reasoningEffort:'medium',revision:0},path='/api/profile/prompt-models/cover_letter';
 assert.equal((await req('Nastya',path,'PUT',config)).status,403);
 assert.equal((await req('Eugene',path,'PUT',{...config,openaiModel:'gpt-6.1-luna',reasoningEffort:'auto'})).status,400);
 assert.equal((await req('Eugene',path,'PUT',{...config,openaiModel:'gpt-6.1-sol',reasoningEffort:'none'})).status,400);
 assert.equal((await req('Eugene',path,'PUT',config)).status,200);
 assert.equal((await req('Eugene','/api/openai/test','POST')).status,200);assert.deepEqual(calls.at(-1).reasoning,{effort:'medium'});assert.equal(calls.at(-1).store,false);assert.equal(calls.at(-1).model,'gpt-6-luna');
 const input={messages:[{role:'system',content:'PROMPT'},{role:'user',content:'DATA'}],maxTokens:4096,task:'cover_letter',validateContent:c=>!!api.parseCoverLetterAnswer(c)};
 for(const failure of ['rate','network','incomplete','bad','mismatch']){mode=failure;await assert.rejects(()=>api.runOpenAI(env,input,config),error=>{assert.ok(!error.message.includes(env.OPENAI_API_KEY));return true;});}
 mode='normal';await api.runOpenAI(env,input,{openaiModel:'gpt-4.1',reasoningEffort:'auto'});assert.ok(!Object.hasOwn(calls.at(-1),'reasoning'));
 available.unshift('gpt-6.1-luna');r=await req('Eugene','/api/openai/refresh','POST');assert.equal((await r.json()).defaultModel,'gpt-6.1-luna');
 await api.runOpenAI(env,input,{openaiModel:'',reasoningEffort:'medium'});assert.equal(calls.at(-1).model,'gpt-6.1-luna');assert.ok(!Object.hasOwn(calls.at(-1),'reasoning'),'Unknown capabilities use model default');
 // Full candidate flow, same coherent inputs and existing save/lease fencing.
 sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Synthetic'); INSERT INTO company_vacancies(id,company_name,title,url,description_md) VALUES(900001,'Synthetic','ROLE','https://example.invalid/1','VACANCY BODY'),(900002,'Synthetic','ROLE','https://example.invalid/2','VACANCY BODY'); INSERT INTO user_profiles(user_name,resume_md) VALUES('Nastya','PRIVATE RESUME'); INSERT INTO system_prompts(kind,markdown) VALUES('cover_letter','ADMIN PROMPT');");
 r=await req('Nastya','/api/vacancies/900001/cover-letter','POST');assert.equal(r.status,200,await r.clone().text());assert.equal((await r.json()).coverLetterSubject,'OpenAI subject');
 const body=calls.at(-1);assert.match(body.input[0].content,/ADMIN PROMPT/);assert.equal(body.input[0].role,'developer');assert.deepEqual(JSON.parse(body.input[1].content),{resumeMd:'PRIVATE RESUME',vacancyTitle:'ROLE',vacancyMd:'VACANCY BODY'});assert.ok(body.max_output_tokens>=32768);
 const count=calls.length;assert.equal((await req('Nastya','/api/vacancies/900001/cover-letter','POST')).status,200);assert.equal(calls.length,count);
 // Regeneration reads new provider config, effort, prompt and personal resume.
 assert.equal((await req('Eugene',path,'PUT',{...config,openaiModel:'gpt-6.1-sol',reasoningEffort:'high',revision:1})).status,200);
 sqlite.exec("UPDATE user_profiles SET resume_md='UPDATED PRIVATE RESUME',revision=revision+1 WHERE user_name='Nastya'; UPDATE system_prompts SET markdown='UPDATED ADMIN PROMPT',revision=revision+1 WHERE kind='cover_letter';");
 r=await req('Nastya','/api/vacancies/900001/cover-letter/regenerate','POST');assert.equal(r.status,200,await r.clone().text());
 assert.equal(calls.at(-1).model,'gpt-6.1-sol');assert.deepEqual(calls.at(-1).reasoning,{effort:'high'});assert.match(calls.at(-1).input[0].content,/UPDATED ADMIN PROMPT/);assert.equal(JSON.parse(calls.at(-1).input[1].content).resumeMd,'UPDATED PRIVATE RESUME');
 mode='held';const inflight=req('Nastya','/api/vacancies/900002/cover-letter','POST');while(!pending)await new Promise(r=>setTimeout(r,5));
 assert.equal((await req('Eugene',path,'PUT',{...config,provider:'openrouter',revision:2})).status,200);
 release();r=await inflight;assert.equal(r.status,409);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=900002').get().n,0,'Changed provider cannot save stale answer');
 mode='normal';assert.equal((await req('Eugene',path,'PUT',{...config,revision:3})).status,200);
 sqlite.exec("INSERT INTO company_vacancies(id,company_name,title,url,description_md) VALUES(900003,'Synthetic','ROLE','https://example.invalid/3','BODY')");
 mode='held';pending=false;const changingKey=req('Nastya','/api/vacancies/900003/cover-letter','POST');while(!pending)await new Promise(r=>setTimeout(r,5));
 assert.equal((await req('Eugene','/api/openai/key','PUT',{apiKey:secret})).status,200);release();assert.equal((await changingKey).status,409);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_cover_letters WHERE vacancy_id=900003').get().n,0);
 const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.ok(!JSON.stringify(backup).includes(secret));assert.equal(await api.decryptGoogleToken(env.SESSION_SECRET+':openai:v1',backup.storage.tables.find(t=>t.name==='openai_settings').rows[0].key_encrypted),secret);
 const timing=sqlite.prepare("SELECT success_count FROM openrouter_model_metrics WHERE model_id='openai:gpt-6-luna'").get();assert.ok(timing.success_count>0);
 console.log('OpenAI verified: account model catalog/default/recommended/effort, admin/CSRF/secret isolation, direct Responses payload and errors, no fallback, full candidate save, existing letter reuse and config-change fencing.');
}finally{globalThis.fetch=originalFetch;sqlite.close();}
