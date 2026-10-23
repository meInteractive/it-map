import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {prepareBackend,applicationTestIntent} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
await prepareBackend();const backend=await import('../.tests/backend.mjs');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'vacancy-concurrency'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try {
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));
 const catalog={...seed,companies:['Concurrent A','Concurrent B'].map(name=>({...seed.companies[0],name,jobs:[]})),agencies:[{name:'Concurrent agency'}]};
 await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify(catalog)).run();
 for(const name of ['Concurrent A','Concurrent B','Concurrent agency'])await db.batch([db.prepare('INSERT INTO company_crm(company_name) VALUES(?)').bind(name),db.prepare('INSERT INTO company_vacancy_state(company_name) VALUES(?)').bind(name)]);
 const cookies={};for(const name of ['Eugene','Nastya']){const r=await mf.dispatchFetch('https://concurrent.test/auth',{method:'POST',body:new URLSearchParams({login:name,password:'synthetic'}),redirect:'manual'});assert.equal(r.status,303);cookies[name]=r.headers.get('set-cookie').split(';')[0];}
 const req=(path,method='GET',body,user='Eugene')=>mf.dispatchFetch('https://concurrent.test'+path,{method,headers:{cookie:cookies[user],origin:'https://concurrent.test','content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})});
 let r=await req('/api/channels','POST',{name:'Concurrent source',url:'https://t.me/concurrent_source'});const channelId=(await r.json()).channelId;
 const input={companyName:'Concurrent A',entityType:'company',url:'https://concurrent.test/vacancy',title:'Initial',role:'Менеджер проектов',level:'Senior',descriptionMd:'Initial body',salaryFrom:200000,salaryTo:300000,relevance:50,telegramChannelId:channelId,telegramMessageUrl:'https://t.me/concurrent_source/1',telegramContact:'@initial'};
 r=await req('/api/vacancies','POST',input);assert.equal(r.status,201,await r.clone().text());const id=(await r.json()).vacancies.find(v=>v.url===input.url).id,path='/api/vacancies/'+id;
 await db.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').bind('Eugene',id,'Own subject','Own private').run();
 await db.prepare('INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown) VALUES(?,?,?,?)').bind('Nastya',id,'Other subject','Other private').run();
 await db.prepare('INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance) VALUES(?,?,?)').bind('Nastya',id,40).run();
 assert.equal((await req(path+'/apply','POST',{companyName:input.companyName,...await applicationTestIntent(()=>req('/api/applications/create-context'))})).status,200);
 await db.prepare('INSERT INTO company_tasks(user_name,company_name,vacancy_id,type,note,due_at) VALUES(?,?,?,?,?,?)').bind('Eugene','Concurrent A',id,'call','Keep task','2026-10-05T12:00:00Z').run();
 const read=async(user='Eugene')=>(await (await req(path,'GET',undefined,user)).json()).vacancy;
 const payload=v=>({...input,...v,telegramChannelId:v.telegramSources[0]?.channelId??null,telegramMessageUrl:v.telegramSources[0]?.messageUrl||'',telegramContact:v.telegramSources[0]?.telegramContact||''});
 const tables=['company_vacancies','vacancy_cover_letters','company_vacancy_user_state','vacancy_telegram_sources','company_applications','company_application_status_history','company_tasks','app_catalog'];
 const snapshot=async()=>Promise.all(tables.map(async table=>(await db.prepare('SELECT * FROM '+table+' ORDER BY rowid').all()).results));
 const original=await read();assert.match(original.revision,/^[a-f0-9]{64}$/);assert.notEqual(original.revision,(await read('Nastya')).revision);
 const before=await snapshot();assert.equal((await req(path,'PUT',input)).status,428);assert.equal((await req(path,'PUT',{...input,revision:1})).status,428);assert.deepEqual(await snapshot(),before);
 assert.equal((await req(path,'PUT',payload(original),'Nastya')).status,403);
 const list=await (await req('/api/vacancies/list')).json();assert.ok(!Object.hasOwn(list.vacancies.find(v=>v.id===id),'revision'));assert.ok(!Object.hasOwn(list.vacancies.find(v=>v.id===id),'descriptionMd'));
 r=await req(path,'PUT',{...payload(original),title:'First editor'});assert.equal(r.status,200,await r.clone().text());
 const afterWinner=await snapshot();assert.equal((await req(path,'PUT',{...payload(original),salaryFrom:250000})).status,409);assert.deepEqual(await snapshot(),afterWinner);
 // State changes in the same timestamp second still change the token.
 const sameSecond=await read();await db.prepare('UPDATE company_vacancies SET title=? WHERE id=?').bind('No timestamp update',id).run();assert.equal((await read()).updatedAt,sameSecond.updatedAt);assert.notEqual((await read()).revision,sameSecond.revision);assert.equal((await req(path,'PUT',payload(sameSecond))).status,409);
 // Other user's data and own favorite/hidden do not invalidate editable state.
 const unaffected=await read();await db.prepare('UPDATE vacancy_cover_letters SET markdown=? WHERE user_name=? AND vacancy_id=?').bind('Other updated','Nastya',id).run();await db.prepare('UPDATE company_vacancy_user_state SET relevance=99 WHERE user_name=? AND vacancy_id=?').bind('Nastya',id).run();assert.equal((await req(path+'/state','PUT',{favorite:true})).status,200);assert.equal((await req(path+'/state','PUT',{hidden:true})).status,200);assert.equal((await read()).revision,unaffected.revision);
 // Own relevance and hidden-by-shared own letter still participate in the CAS.
 let base=await read();await db.prepare('UPDATE company_vacancy_user_state SET relevance=88 WHERE user_name=? AND vacancy_id=?').bind('Eugene',id).run();assert.equal((await req(path,'PUT',payload(base))).status,409);
 base=await read();assert.equal((await req(path,'PUT',{...payload(base),coverLetterScope:'shared',coverLetterSubject:'Shared','coverLetterMd':'Shared body'})).status,200);
 base=await read();assert.equal(base.coverLetterScope,'shared');await db.prepare('UPDATE vacancy_cover_letters SET markdown=? WHERE user_name=? AND vacancy_id=?').bind('New hidden own','Eugene',id).run();assert.equal((await read()).coverLetterMd,base.coverLetterMd);assert.notEqual((await read()).revision,base.revision);assert.equal((await req(path,'PUT',{...payload(base),coverLetterScope:'personal',coverLetterSubject:'Own overwrite',coverLetterMd:'Stale own'})).status,409);
 base=await read();await db.prepare('UPDATE vacancy_telegram_sources SET telegram_contact=? WHERE vacancy_id=?').bind('@fresh_source',id).run();assert.equal((await req(path,'PUT',payload(base))).status,409);
 // Force races after repository preflight. Every dependent table must remain
 // exactly as the external winner left it, including transfer and catalogue.
 for(const mutation of [
  ()=>db.prepare('UPDATE company_vacancies SET title=? WHERE id=?').bind('External winner',id).run(),
  ()=>db.prepare('UPDATE company_vacancy_user_state SET relevance=77 WHERE user_name=? AND vacancy_id=?').bind('Eugene',id).run(),
  ()=>db.prepare('UPDATE vacancy_cover_letters SET markdown=? WHERE user_name=? AND vacancy_id=?').bind('External private','Eugene',id).run(),
  ()=>db.prepare('UPDATE vacancy_telegram_sources SET telegram_contact=? WHERE vacancy_id=?').bind('@external_source',id).run(),
 ]){
  base=await read();let expected,hit=0;const guardedDb={prepare:db.prepare.bind(db),async batch(items){if(!items.some(item=>String(item.statement||item.sql||'').includes('required_update_guard')))return db.batch(items);hit++;await mutation();expected=await snapshot();return db.batch(items);}};
  await assert.rejects(()=>backend.updateVacancy(guardedDb,'Eugene',id,{...payload(base),companyName:'Concurrent agency',entityType:'agency',title:'Loser',relevance:1,coverLetterScope:'personal',coverLetterSubject:'Loser','coverLetterMd':'Loser',telegramContact:'@loser'}),error=>error.status===409);
  assert.equal(hit,1,'Forced change must happen at the mutation batch');assert.deepEqual(await snapshot(),expected);
 }
 // A real late integer overflow is a normal 400, not a fake conflict; rollback
 // keeps the original revision available for a successful retry.
 base=await read();const beforeOverflow=await snapshot();
 await db.prepare('CREATE TRIGGER late_overflow BEFORE INSERT ON company_vacancy_user_state BEGIN SELECT abs(-9223372036854775808); END').run();
 r=await req(path,'PUT',{...payload(base),companyName:'Concurrent B',title:'Should roll back',coverLetterScope:'personal',coverLetterSubject:'Changed','coverLetterMd':'Changed',telegramContact:'@rollback'});assert.equal(r.status,400,await r.clone().text());assert.deepEqual(await snapshot(),beforeOverflow);assert.equal((await read()).revision,base.revision);
 await db.prepare('DROP TRIGGER late_overflow').run();assert.equal((await req(path,'PUT',{...payload(base),title:'Retry works'})).status,200);
 base=await read();const race=await Promise.all(['Parallel A','Parallel B'].map(title=>req(path,'PUT',{...payload(base),title})));assert.deepEqual(race.map(r=>r.status).sort(),[200,409]);
 // Deletion after preflight rolls back attempted dependant writes too.
 base=await read();let deletedState;const deletingDb={prepare:db.prepare.bind(db),async batch(items){if(!items.some(item=>String(item.statement||item.sql||'').includes('required_update_guard')))return db.batch(items);await db.prepare('DELETE FROM company_vacancies WHERE id=?').bind(id).run();deletedState=await snapshot();return db.batch(items);}};
 await assert.rejects(()=>backend.updateVacancy(deletingDb,'Eugene',id,payload(base)),error=>error.status===409);assert.deepEqual(await snapshot(),deletedState);assert.equal((await req(path,'PUT',payload(base))).status,404);
 console.log('Real D1/workerd: strict revision, stale/same-second saves, scoped state/source fingerprints, forced post-preflight races, all dependant rollback, genuine overflow classification, retry, parallel requests and deletion passed.');
} finally {await mf.dispose();}
