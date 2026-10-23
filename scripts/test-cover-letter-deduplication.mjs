import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const waiting=[],calls=[];
const models=['first','next'].map(id=>({id:'synthetic/'+id,name:id,context_length:262144,supported_parameters:['reasoning'],architecture:{output_modalities:['text']},pricing:{prompt:0,completion:0}}));
async function provider(url,body){
 if(url.includes('/models?'))return Response.json({data:models});
 if(url.includes('/benchmarks?'))return Response.json({data:[]});
 if(url.endsWith('/chat/completions')){
  const request=JSON.parse(body),input=JSON.parse(request.messages[1].content);calls.push({model:request.model,...input});
  await new Promise(resolve=>waiting.push(resolve));
  return Response.json({model:request.model,choices:[{message:{content:JSON.stringify({subject:'For '+input.resumeMd,markdown:'Letter for '+input.resumeMd+' / '+input.vacancyTitle})}}]});
 }
 throw new Error('Unexpected provider request');
}
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'letter-dedupe'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:async request=>provider(request.url,await request.text())};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB','first'),workers={first:await mf.getWorker('first'),second:await mf.getWorker('second')};
 await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();
 await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 for(const id of [1,2])await db.prepare("INSERT INTO company_vacancies(id,company_name,url,title,description_md) VALUES(?,'Synthetic',?,?,'Body')").bind(id,'https://dedupe.test/'+id,'Role '+id).run();
 const cookies={};for(const name of ['Eugene','Nastya']){const r=await workers.first.fetch('https://dedupe.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Eugene',instance='first',headers={})=>workers[instance].fetch('https://dedupe.test'+path,{method,headers:{cookie:cookies[user],'content-type':'application/json',...headers},...(body===undefined?{}:{body:JSON.stringify(body)})});
 for(const user of ['Eugene','Nastya'])assert.equal((await req('/api/profile/resume','PUT',{resumeMd:'CV '+user,revision:0},user)).status,200);
 assert.equal((await req('/api/profile/prompts/cover_letter','PUT',{markdown:'Prompt',revision:0})).status,200);
 assert.equal((await req('/api/openrouter/key','PUT',{apiKey:'sk-or-synthetic-1234567890'})).status,200);
 const letter=(instance='first',user='Nastya',id=1,stream=false)=>req('/api/vacancies/'+id+'/cover-letter','POST',undefined,user,instance,stream?{accept:'application/x-ndjson'}:{});
 const wait=async(n=1)=>{for(let i=0;i<2000&&waiting.length<n;i++)await new Promise(r=>setTimeout(r,1));assert.equal(waiting.length,n)};
 const release=()=>waiting.shift()();
 // One JSON owner, one NDJSON joiner in an independent Worker. Observe waiting before release.
 const owner=letter();await wait();const streamed=await letter('second','Nastya',1,true),reader=streamed.body.getReader();let frames='';
 while(!frames.includes('"stage":"waiting"')){const part=await reader.read();assert.ok(!part.done);frames+=new TextDecoder().decode(part.value);}
 assert.equal(calls.length,1);assert.equal(waiting.length,1);release();const response=await owner;assert.equal(response.status,200,await response.clone().text());const result=await response.json();
 for(;;){const part=await reader.read();if(part.done)break;frames+=new TextDecoder().decode(part.value);}
 const done=frames.trim().split('\n').map(JSON.parse).find(e=>e.type==='done');assert.deepEqual(done.letter,result);assert.equal(calls.length,1);
 assert.equal((await db.prepare('SELECT count(*) n FROM vacancy_cover_letters').first()).n,1);
 assert.equal((await db.prepare('SELECT sum(success_count) n FROM openrouter_model_metrics').first()).n,1);
 assert.equal((await db.prepare("SELECT count(*) n FROM openrouter_ai_logs WHERE task='cover_letter_attempt'").first()).n,1,'One timing/log per actual provider attempt');
 assert.equal((await db.prepare('SELECT count(*) n FROM cover_letter_generation_leases').first()).n,0,'Owner released after completion');
 assert.equal((await letter('second')).status,200);assert.equal(calls.length,1,'Saved result never regenerates');
 // Both independent candidates get their own letters, never a coalesced private result.
 await db.prepare('DELETE FROM vacancy_cover_letters').run();let a=letter(),b=letter('second','Eugene');await wait(2);release();release();const pair=await Promise.all([a,b]);assert.ok(pair.every(r=>r.status===200));const personal=await Promise.all(pair.map(r=>r.json()));assert.match(personal[0].coverLetterMd,/CV Nastya/);assert.match(personal[1].coverLetterMd,/CV Eugene/);assert.notEqual(personal[0].coverLetterMd,personal[1].coverLetterMd);
 // Different vacancies also remain independent.
 await db.prepare('DELETE FROM vacancy_cover_letters').run();a=letter();b=letter('second','Nastya',2);await wait(2);release();release();assert.ok((await Promise.all([a,b])).every(r=>r.status===200));assert.equal(calls.length,5);
 console.log('Two real workerd Workers + native D1: one provider call, identical JSON/NDJSON result, waiting progress, one letter/timing/log, saved no regeneration, candidate privacy and vacancy isolation passed.');
}finally{for(const release of waiting.splice(0))release();await mf.dispose();}
