import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {prepareBackend,moduleFor,nav} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const api=await import('../.tests/backend.mjs'),ui=await moduleFor('scripts/react-test-entry.tsx');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const bindings={SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'};
const worker={modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'apply-protected'},bindings,outboundService:()=>new Response('Disabled',{status:503})};
const options={cf:false,workers:[{...worker,name:'first'},{...worker,name:'second'}]},mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
const base='https://apply-protected.test',route=id=>'/api/vacancies/'+id+'/apply/create';
const oldFetch=fetch;let root;
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
try {
 const db=await mf.getD1Database('DB','first'),workers=[await mf.getWorker('first'),await mf.getWorker('second')];await db.batch(buildBackupSchema(process.cwd()).objects.map(object=>db.prepare(object.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:['Synthetic','Limited','Other'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[{...seed.agencies?.[0],name:'Recruiter',jobs:[]}]})).run();
 for(const [id,name,type] of [[1,'Synthetic','company'],[2,'Synthetic','company'],[3,'Synthetic','company'],[4,'Recruiter','agency'],[5,'__telegram_channel__:1','channel'],[6,'Limited','company'],[7,'Synthetic','company'],[8,'Synthetic','company'],[9,'Synthetic','company']]){
  await db.prepare('INSERT OR IGNORE INTO company_crm(company_name) VALUES(?)').bind(name).run();await db.prepare('INSERT INTO company_vacancies(id,company_name,entity_type,url,title) VALUES(?,?,?,?,?)').bind(id,name,type,'https://synthetic.invalid/'+id,'Synthetic '+id).run();
 }
 await db.prepare("INSERT INTO telegram_channels(id,name,url,username,normalized_username) VALUES(1,'Channel','https://t.me/synthetic','synthetic','synthetic')").run();await db.prepare('INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url) VALUES(5,1,?)').bind('https://t.me/synthetic/1').run();
 const cookies={};for(const name of ['Eugene','Nastya']){const response=await workers[0].fetch(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(response.status,303);cookies[name]=response.headers.get('set-cookie').split(';')[0];}
 const req=(index,path,method='POST',body,user='Nastya')=>workers[index].fetch(base+path,{method,headers:{cookie:cookies[user]||'','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 const context=async(user='Nastya')=>(await(await req(0,'/api/applications/create-context','GET',undefined,user)).json()).creationContext;
 let creationContext=await context();const intent=(companyName='Synthetic')=>({companyName,creationContext,idempotencyKey:crypto.randomUUID()});
 const snapshot=async()=>Promise.all(['company_applications','company_application_status_history','application_creation_receipts'].map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 let before=await snapshot();assert.equal((await req(0,route(1),'POST',undefined,'unknown')).status,401);assert.equal((await req(0,route(1),'GET')).status,405);
 for(const body of [{companyName:'Synthetic'},{companyName:'Synthetic',creationContext},{companyName:'Synthetic',idempotencyKey:crypto.randomUUID()}])assert.equal((await req(0,route(1),'POST',body)).status,428);
 for(const body of [undefined,{}, {companyName:'Synthetic',creationContext}])assert.equal((await req(0,'/api/vacancies/1/apply','POST',body)).status,428);
 assert.equal((await req(0,'/api/vacancies/1/apply','POST',{},'unknown')).status,401);assert.equal((await req(0,'/api/vacancies/1/apply','GET')).status,405);
 assert.equal((await mf.dispatchFetch(base+'/api/vacancies/1/apply',{method:'POST',headers:{cookie:cookies.Nastya,origin:base},body:'{'})).status,400);
 assert.equal((await mf.dispatchFetch(base+'/api/vacancies/1/apply',{method:'POST',headers:{cookie:cookies.Nastya,origin:'https://foreign.test'},body:JSON.stringify(intent())})).status,403);
 assert.equal((await mf.dispatchFetch(base+route(1),{method:'POST',headers:{cookie:cookies.Nastya,origin:'https://foreign.test'},body:JSON.stringify(intent())})).status,403);assert.deepEqual(await snapshot(),before);
 const original=intent();let response=await req(0,route(1),'POST',{...original,userName:'Eugene',comment:'Ignored',status:'Офер'});assert.equal(response.status,200,await response.clone().text());const first=await response.json();assert.ok(first.ok);assert.equal(first.vacancy.applicationId,first.createdApplicationId);assert.equal(first.vacancy.applicationStatus,'Откликнулся');assert.equal(first.replayed,false);assert.deepEqual((await snapshot()).map(rows=>rows.length),[1,1,1]);
 response=await req(1,route(1),'POST',original);assert.equal(response.status,200);assert.equal((await response.json()).createdApplicationId,first.createdApplicationId);assert.deepEqual((await snapshot()).map(rows=>rows.length),[1,1,1]);
 response=await req(1,'/api/vacancies/1/apply','POST',original);assert.equal(response.status,200);assert.equal((await response.json()).createdApplicationId,first.createdApplicationId);assert.deepEqual((await snapshot()).map(rows=>rows.length),[1,1,1]);
 before=await snapshot();assert.equal((await req(0,route(2),'POST',original)).status,409);assert.equal((await req(0,route(1),'POST',{...original,companyName:'Other'})).status,409);assert.deepEqual(await snapshot(),before);
 const race=intent();const parallel=await Promise.all([req(0,'/api/vacancies/2/apply','POST',race),req(1,route(2),'POST',race)]);assert.ok(parallel.every(response=>response.status===200));const results=await Promise.all(parallel.map(response=>response.json()));assert.equal(results[0].createdApplicationId,results[1].createdApplicationId);
 const different=await Promise.all([req(0,route(3),'POST',intent()),req(1,route(3),'POST',intent())]);assert.ok(different.every(response=>response.status===200));const differentResults=await Promise.all(different.map(response=>response.json()));assert.equal(differentResults[0].createdApplicationId,differentResults[1].createdApplicationId);
 // Fresh shortcut intents remember an existing record, preserving all its edits.
 await db.prepare("UPDATE company_applications SET status='Офер',comment_md='Preserved',recruiter_name='Recruiter' WHERE id=?").bind(first.createdApplicationId).run();before=await snapshot();const existingIntent=intent();response=await req(0,route(1),'POST',existingIntent);assert.equal(response.status,200);assert.equal((await response.json()).createdApplicationId,first.createdApplicationId);assert.deepEqual((await snapshot()).slice(0,2),before.slice(0,2));
 before=await snapshot();assert.equal((await req(0,route(1),'POST',intent('Other'))).status,409);assert.deepEqual(await snapshot(),before);
 await db.prepare("CREATE TRIGGER fail_apply_receipt BEFORE INSERT ON application_creation_receipts WHEN NEW.created_application_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'Synthetic link failure'); END").run();
 const linkFailure=intent();assert.equal((await req(0,route(1),'POST',linkFailure)).status,503);assert.deepEqual(await snapshot(),before);await db.prepare('DROP TRIGGER fail_apply_receipt').run();assert.equal((await req(1,route(1),'POST',linkFailure)).status,200);
 // Another user cannot replay the owner's context or see their private result.
 before=await snapshot();assert.equal((await req(1,route(1),'POST',original,'Eugene')).status,409);assert.deepEqual(await snapshot(),before);response=await req(1,route(1),'POST',{...intent(),creationContext:await context('Eugene')},'Eugene');assert.equal(response.status,200);assert.notEqual((await response.json()).createdApplicationId,first.createdApplicationId);
 for(const [id,name] of [[4,'Recruiter'],[5,'__telegram_channel__:1']]){response=await req(0,route(id),'POST',intent(name));assert.equal(response.status,200,await response.clone().text());assert.equal((await response.json()).vacancy.companyName,name);}
 await db.batch(Array.from({length:9},()=>db.prepare("INSERT INTO company_applications(user_name,company_name,status) VALUES('Nastya','Limited','')")));const tenth=intent('Limited');assert.equal((await req(0,route(6),'POST',tenth)).status,200);assert.equal((await req(1,route(6),'POST',tenth)).status,200);assert.equal((await req(1,route(6),'POST',intent('Limited'))).status,200,'Fresh intent can obtain existing tenth record');
 await req(0,'/api/applications/'+first.createdApplicationId,'DELETE');before=await snapshot();for(const body of [original,existingIntent])assert.equal((await req(1,route(1),'POST',body)).status,410);assert.deepEqual(await snapshot(),before);
 // Late history failure rolls back the receipt too; the original intent can retry.
 await db.prepare("CREATE TRIGGER fail_apply_history BEFORE INSERT ON company_application_status_history BEGIN SELECT RAISE(ABORT,'Synthetic failure'); END").run();before=await snapshot();const failure=intent();assert.equal((await req(0,route(7),'POST',failure)).status,503);assert.deepEqual(await snapshot(),before);await db.prepare('DROP TRIGGER fail_apply_history').run();assert.equal((await req(1,route(7),'POST',failure)).status,200);
 // Actual React button against production HTTP/native D1 drops the first ACK.
 let gets=0,drop=true;const sent=[];globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications/create-context'){gets++;return req(0,url,'GET');}
  sent.push(JSON.parse(options.body));const response=await req(0,url,options.method,JSON.parse(options.body));if(drop&&response.ok){drop=false;throw new TypeError('Synthetic lost ACK');}return response;
 };
 await act(async()=>{root=create(h(ui.VacancyActions,{v:{id:8,companyName:'Synthetic',title:'Synthetic',applicationId:null},entities:[],admin:false,nav}));});assert.equal(gets,0);
 const click=async()=>{await act(async()=>root.root.findByProps({className:'vacancy-apply'}).props.onClick());for(let i=0;i<1000&&root.root.findByProps({className:'vacancy-apply'}).props.disabled;i++)await act(async()=>{await new Promise(resolve=>setTimeout(resolve,2));});assert.equal(root.root.findByProps({className:'vacancy-apply'}).props.disabled,false);};
 await click();assert.match(JSON.stringify(root.toJSON()),/lost ACK/);before=await snapshot();await click();assert.deepEqual(await snapshot(),before);assert.deepEqual(sent[0],sent[1]);assert.equal(gets,1);assert.equal(before[0].filter(row=>row.vacancy_id===8&&row.user_name==='Nastya').length,1);await act(async()=>root.unmount());root=null;globalThis.fetch=oldFetch;
 const backup=await api.createBackup(db),stale=intent();await api.restoreBackup(db,backup);before=await snapshot();assert.equal((await req(0,route(9),'POST',stale)).status,409);assert.equal((await req(0,route(2),'POST',race)).status,409);assert.deepEqual(await snapshot(),before);
 // Force owner change or restore at the actual batch, through the real handler
 // and native D1. Neither linking nor creating may cross that boundary.
 function proxy(hook){const entries=new WeakMap();function wrap(native,sql){const statement={bind(...args){return wrap(native.bind(...args),sql);},first(...args){return native.first(...args);},all(){return native.all();},run(){return native.run();}};entries.set(statement,{native,sql});return statement;}return {prepare(sql){return wrap(db.prepare(sql),sql);},async batch(statements){return hook(statements.map(statement=>entries.get(statement).sql),()=>db.batch(statements.map(statement=>entries.get(statement).native)));}};}
 const nodeReq=(connection,id,body)=>api.default.fetch(new Request(base+route(id),{method:'POST',headers:{cookie:cookies.Nastya,origin:base,'content-type':'application/json'},body:JSON.stringify(body)}),{...bindings,DB:connection});
 creationContext=await context();before=await snapshot();let changed=false;const ownerRace=proxy(async(sql,run)=>{if(!changed&&sql.some(query=>query.startsWith('INSERT INTO application_creation_receipts'))){changed=true;await db.prepare("UPDATE company_vacancies SET company_name='Other',real_company_name='Synthetic' WHERE id=9").run();}return run();});assert.equal((await nodeReq(ownerRace,9,intent())).status,503);assert.ok(changed);assert.deepEqual(await snapshot(),before);await api.restoreBackup(db,backup);
 for(const when of ['before','after']){creationContext=await context();let restored=false;const connection=proxy(async(sql,run)=>{const claim=sql.some(query=>query.startsWith('INSERT INTO application_creation_receipts'));if(claim&&when==='before'){await api.restoreBackup(db,backup);restored=true;}const result=await run();if(claim&&when==='after'){await api.restoreBackup(db,backup);restored=true;}return result;});assert.equal((await nodeReq(connection,2,intent())).status,409);assert.ok(restored);assert.deepEqual((await api.createBackup(db)).storage,backup.storage);}
 console.log('Protected shortcut and strict old alias, actual HTTP/two Workers/native D1 and React: missing428/no writes/privacy/Origin/JSON, cross-URL replay/race, lost ACK, distinct-key reuse, existing fields/history preserved, owners, tenth-slot, delete410, rollback, owner race and restore before/after commit409 passed.');
} finally {if(root)await act(async()=>root.unmount());ui.clearCache();globalThis.fetch=oldFetch;await mf.dispose();}
