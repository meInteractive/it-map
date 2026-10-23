import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {prepareBackend,moduleFor} from './test-support.mjs';
await prepareBackend();
const api=await import('../.tests/backend.mjs'),worker=api.default;
const sqlite=new DatabaseSync(':memory:');
for(const f of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+f,'utf8'));
const db={prepare(sql){let values=[];return {bind(...v){values=v;return this;},async first(){return sqlite.prepare(sql).get(...values)||null;},async all(){return {results:sqlite.prepare(sql).all(...values)};},async run(){const q=sqlite.prepare(sql);return q.columns().length?{results:q.all(...values)}:{meta:q.run(...values)};}};},async batch(statements){sqlite.exec('BEGIN');try{const out=[];for(const s of statements)out.push(await s.run());sqlite.exec('COMMIT');return out;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
const env={DB:db,SITE_PASSWORD:'fixture',NASTYA_PASSWORD:'fixture',SESSION_SECRET:'fixture-secret'};
const base='https://jev-test.invalid',cookies={};
for(const name of ['Eugene','Nastya']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'fixture'})}),env);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
const path='/api/profile/prompt-models/relevance';
const req=(name,url=path,body,origin=base)=>worker.fetch(new Request(base+url,{method:body?'PUT':'GET',headers:{cookie:cookies[name],origin,'content-type':'application/json'},body:body?JSON.stringify(body):undefined}),env);
const originalFetch=fetch,calls=[];let mode='normal',root;
globalThis.fetch=async(url,opts={})=>{
 const body=opts.body?JSON.parse(opts.body):undefined;calls.push({url:String(url),body});
 if(String(url).includes('/models?')){
  assert.match(String(url),/[?&]output_modalities=decisions(?:&|$)/,'JEV catalog must request decision models explicitly');
  return Response.json({data:mode==='missing'?[]:[{id:api.JEV_MODEL_ID,name:'TypeSafe: Jev 1.13',architecture:{output_modalities:['decisions']},pricing:{prompt:mode==='free'?'0':'0.000000042',completion:'0'},context_length:64000}]});
 }
 if(String(url).endsWith('/api/alpha/decisions')){
  if(mode==='credits')return Response.json({error:{code:402}},{status:402});
  if(mode==='bad')return Response.json({model:api.JEV_MODEL_ID,answers:{relevance:{type:'score',score:99}}});
  return Response.json({model:mode==='mismatch'?'another/model':api.JEV_MODEL_ID+'-20260917',answers:{relevance:{type:'score',score:7.2,confidence:.9}},usage:{cost:.00002}});
 }
 throw new Error('Unexpected request '+url);
};
try{
 const initial=await (await req('Eugene','/api/profile/prompt-models')).json();assert.equal(initial.models.relevance.openrouterModel,'best_free');
 const config={provider:'openrouter',openaiModel:'',openrouterModel:'jev',reasoningEffort:'medium',revision:0};
 assert.equal((await req('Nastya',path,config)).status,403);
 assert.equal((await req('Eugene',path,config,'https://evil.invalid')).status,403);
 assert.equal((await req('Eugene','/api/profile/prompt-models/cover_letter',config)).status,400);
 assert.equal((await req('Eugene',path,{...config,openrouterModel:'other/model'})).status,400);
 // Existing OpenRouter credentials are reused; saving the choice performs no inference.
 const encrypted=await api.encryptGoogleToken(env.SESSION_SECRET+':openrouter:v1','sk-or-SYNTHETIC-JEV-KEY');
 await api.orSettings(env);sqlite.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').run(encrypted);
 for(const failure of ['missing','free']){mode=failure;assert.equal((await req('Eugene',path,config)).status,409);assert.equal(sqlite.prepare("SELECT COUNT(*) n FROM prompt_model_settings WHERE kind='relevance'").get().n,0);}
 mode='normal';const catalog=await req('Eugene','/api/openrouter/jev');assert.equal(catalog.status,200);const jev=await catalog.json();assert.equal(jev.id,api.JEV_MODEL_ID);assert.equal(jev.pricing.prompt,'0.000000042');assert.match(jev.source,/openrouter\.ai/);assert.ok(jev.fetchedAt);
 const saved=await req('Eugene',path,config);assert.equal(saved.status,200,await saved.clone().text());assert.equal((await saved.json()).openrouterModel,'jev');
 const roleSaved=await req('Eugene','/api/profile/prompt-models/vacancy_role',{...config,revision:initial.models.vacancy_role.revision});assert.equal(roleSaved.status,200,await roleSaved.clone().text());assert.equal((await roleSaved.json()).openrouterModel,'jev');assert.equal((await req('Eugene','/api/profile/prompt-models/vacancy_extraction',config)).status,400);
 assert.ok(calls.every(c=>!c.body),'Model selection never makes a paid Decisions request');
 assert.equal((await req('Eugene',path,config)).status,409,'Stale revision cannot overwrite another tab');
 const settings=(await (await req('Eugene','/api/profile/prompt-models')).json()).models.relevance;
 const options={task:'relevance',instructions:'Compare confirmed responsibilities with this resume.',state:{resume:'Team manager',vacancy:'Development manager'}};
 const result=await api.runPromptAI(env,options,settings);assert.equal(result.relevance,80);assert.equal(result.fallback_used,false);
 const payload=calls.at(-1);assert.ok(payload.url.endsWith('/api/alpha/decisions'));assert.equal(payload.body.model,api.JEV_MODEL_ID);assert.equal(payload.body.questions.relevance.type,'score');assert.deepEqual(payload.body.state,options.state);assert.ok(!payload.body.messages&&!payload.body.provider);
 for(const failure of ['bad','mismatch','credits']){mode=failure;const start=calls.length;await assert.rejects(()=>api.runPromptAI(env,options,settings));assert.equal(calls.length-start,2,'One catalog read plus one JEV request; no fallback');}
 await assert.rejects(()=>api.runPromptAI(env,{...options,task:'cover_letter'},settings));
 mode='normal';const backup=await api.createBackup(db);assert.equal(backup.version,39);assert.equal(backup.storage.tables.find(t=>t.name==='prompt_model_settings').rows[0].openrouter_model,'jev');assert.ok(!JSON.stringify(backup).includes('sk-or-SYNTHETIC-JEV-KEY'));
 // The native UI saves JEV, preserves it when switching providers, and restores it after reload.
 const {PromptModel}=await moduleFor('frontend/features/settings/PromptModel.tsx');
 globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.IS_REACT_ACT_ENVIRONMENT=true;
 let sent;
 globalThis.fetch=async(url,opts={})=>{if(opts.body){sent=JSON.parse(opts.body);return Response.json({...sent,kind:'relevance',revision:2,updatedAt:null});}if(String(url).endsWith('/api/openrouter/jev'))return Response.json({id:api.JEV_MODEL_ID,name:'TypeSafe: Jev 1.13',pricing:{prompt:'0.000000042',completion:'0'},source:'https://openrouter.ai/'+api.JEV_MODEL_ID,fetchedAt:'2026-10-12T00:00:00.000Z'});return Response.json({models:[{id:'gpt-4.1',name:'GPT-4.1',reasoningEfforts:[]}],defaultModel:'gpt-4.1'});};
 await act(async()=>{root=create(h(PromptModel,{initial:{...settings,openrouterModel:'best_free'}}));});
 const selector=()=>root.root.findAllByType('select').find(n=>n.props['aria-label']==='Модель OpenRouter');
 assert.deepEqual(selector().findAllByType('option').map(n=>n.children.join('')),['Лучшая бесплатная','JEV']);
 assert.equal(root.root.findByProps({className:'prompt-cost'}).findByType('strong').children.join(''),'≈ 0,00 ₽');
 await act(async()=>selector().props.onChange({target:{value:'jev'}}));await act(async()=>{});
 assert.equal(root.root.findByProps({className:'prompt-cost'}).findByType('strong').children.join(''),'≈ 26,78 ₽');assert.match(JSON.stringify(root.toJSON()),/15 000 русских символов/);
 await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.equal(sent.openrouterModel,'jev');
 await act(async()=>root.unmount());
 await act(async()=>{root=create(h(PromptModel,{initial:{...settings,openrouterModel:'jev'}}));});assert.equal(selector().props.value,'jev');
 const provider=()=>root.root.findAllByType('select').find(n=>n.props['aria-label']==='Провайдер');
 await act(async()=>provider().props.onChange({target:{value:'openai'}}));await act(async()=>provider().props.onChange({target:{value:'openrouter'}}));assert.equal(selector().props.value,'jev');
 await act(async()=>root.unmount());await act(async()=>{root=create(h(PromptModel,{initial:{...settings,kind:'vacancy_role',openrouterModel:'best_free'}}));});assert.ok(selector());await act(async()=>selector().props.onChange({target:{value:'jev'}}));await act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));assert.equal(sent.openrouterModel,'jev');assert.match(JSON.stringify(root.toJSON()),/определения конечных ролей/);
 await act(async()=>root.unmount());
 await act(async()=>{root=create(h(PromptModel,{initial:{...settings,kind:'cover_letter',openrouterModel:'best_free'}}));});assert.equal(selector(),undefined);
 console.log('JEV verified: relevance/leaf-role selection, native form persistence, admin/CSRF/CAS, paid catalog validation, exact Decisions model/score, no fallback, backup and secret isolation.');
}finally{if(root)await act(async()=>root.unmount());globalThis.fetch=originalFetch;sqlite.close();}
