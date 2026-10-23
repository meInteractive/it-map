import assert from 'node:assert/strict';
import {fileURLToPath} from 'node:url';
import {prepareBackend} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();const api=await import('../.tests/backend.mjs');
let requests=0,hold=false,release,pending;
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'isolated-vacancy-candidates'},bindings:{SITE_PASSWORD:'synthetic',SESSION_SECRET:'candidate-d1-secret'},outboundService:async request=>{
 const path=new URL(request.url).pathname;
 if(path==='/api/v1/models')return Response.json({data:[{id:api.JEV_MODEL_ID,pricing:{prompt:'0.000000042'}}]});
 assert.equal(path,'/api/alpha/decisions');requests++;const body=await request.json();
 if(hold)await new Promise(resolve=>{pending=true;release=resolve;});
 return Response.json({model:api.JEV_MODEL_ID,answers:Object.fromEntries(Object.keys(body.questions).map((key,i)=>[key,{type:'choice',choice:i===0?'match':'reject'}])),usage:{cost:.00002}});
}};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const login=await mf.dispatchFetch('https://candidate.test/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});assert.equal(login.status,303);const cookie=login.headers.get('set-cookie').split(';')[0];
 const req=(path,method='GET',body)=>mf.dispatchFetch('https://candidate.test'+path,{method,headers:{cookie,origin:'https://candidate.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 assert.equal((await req('/api/classification/settings')).status,200);
 const encrypted=await api.encryptGoogleToken('candidate-d1-secret:openrouter:v1','SYNTHETIC-KEY');await db.prepare('INSERT OR IGNORE INTO openrouter_settings(id) VALUES(1)').run();await db.prepare('UPDATE openrouter_settings SET key_encrypted=? WHERE id=1').bind(encrypted).run();
 const payload={titleRaw:'Senior Engineering Manager',vacancyUrl:'https://isolated.test/job?id=41&utm_source=fixture'};
 const saved=await Promise.all(Array.from({length:12},async()=>{const r=await req('/api/vacancy-candidates','POST',payload);assert.ok([200,201].includes(r.status),await r.clone().text());return r.json();}));
 assert.equal(new Set(saved.map(r=>r.id)).size,1);assert.equal(saved.filter(r=>r.created).length,1);const id=saved[0].id;
 hold=true;pending=false;const operation=req('/api/vacancy-candidates/'+id+'/classify','POST',{force:false});for(let i=0;i<1000&&!pending;i++)await new Promise(r=>setTimeout(r,1));assert.ok(pending);assert.equal((await req('/api/vacancy-candidates/'+id+'/classify','POST',{force:false})).status,409);hold=false;release();let response=await operation;assert.equal(response.status,200,await response.clone().text());let result=await response.json();assert.equal(result.history.total,2);assert.equal(result.attempts.total,1);assert.equal(result.candidate.processingStatus,'accepted');assert.equal(requests,1);
 response=await req('/api/vacancy-candidates/'+id+'/classify','POST',{});assert.equal((await response.json()).reused,true);assert.equal(requests,1);
 assert.equal((await db.prepare('SELECT count(*) AS n FROM company_vacancies').first()).n,0);
 const backup=await api.createBackup(db);await api.restoreBackup(db,backup);assert.equal((await api.getVacancyCandidate(db,id)).history.total,2);assert.equal((await db.prepare('PRAGMA foreign_key_check').all()).results.length,0);
 console.log('Native workerd/D1 candidates: parallel discovery, one-owner classification, saved decisions/cost, inference cache and backup roundtrip passed; no real network or production data');
}finally{await mf.dispose();}
