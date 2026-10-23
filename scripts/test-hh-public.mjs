import assert from 'node:assert/strict';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const {parsePublicHhJob,fetchPublicHhJob}=await moduleFor('server/integrations/hh-public.js');
const {fetchApifyDetail}=await moduleFor('server/services/apify-vacancies.js');
const description='<p>Вам предстоит:</p><ul><li>Управлять инженерной командой и развивать продукт. '.repeat(10)+'</li></ul><p>Мы ждём: опыт руководства.</p>';
const job={'@type':'JobPosting',identifier:{value:123},title:'Руководитель &amp; команда',description,datePosted:'2026-10-07T12:00:00+03:00',validThrough:'2099-11-01',hiringOrganization:{name:'Компания'}};
const html=j=>'<script type="application/ld+json">'+JSON.stringify(j)+'</script>';
assert.equal(parsePublicHhJob(html(job),123).date_posted,'2026-10-07');assert.match(parsePublicHhJob(html(job),123).description_text,/- Управлять/);
for(const value of [{...job,identifier:{value:124}},{...job,validThrough:'2000-01-01'},{...job,url:'https://evil.test/vacancy/123'},[job,job]])assert.equal(parsePublicHhJob(html(value),123),null);
const original=globalThis.fetch;let calls=0;
try{
 globalThis.fetch=async(url,o)=>{calls++;assert.equal(url,'https://hh.ru/vacancy/123');assert.equal(o.redirect,'manual');assert.equal(o.headers.Authorization,undefined);return new Response(html(job),{headers:{'content-type':'text/html'}});};
 assert.equal((await fetchPublicHhJob('https://spb.hh.ru/vacancy/123?x=1')).vacancy_id,'123');
 for(const u of ['http://hh.ru/vacancy/123','https://hh.ru.evil.test/vacancy/123','https://name@hh.ru/vacancy/123','https://hh.ru/employer/123'])assert.equal(await fetchPublicHhJob(u),null);
 assert.equal(calls,1,'no fetch for disallowed URLs');
 for(const response of [()=>new Response('',{status:302,headers:{location:'https://evil.test/'}}),()=>new Response('captcha',{status:403}),()=>new Response(html(job),{headers:{'content-type':'application/json'}}),()=>new Response('x'.repeat(2000001),{headers:{'content-type':'text/html'}})]){globalThis.fetch=async()=>response();assert.equal(await fetchPublicHhJob('https://hh.ru/vacancy/123'),null);}
 const f=await schedulerFixture();try{
  const {sqlite,db,env}=f;await f.api.schedulerSettings(db);
  sqlite.exec("INSERT INTO apify_settings(id,enabled) VALUES(1,1); INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,vacancy_url,source_id,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(1,'candidate','Руководитель','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','https://hh.ru/vacancy/123','hh','site','now','now','now','now')");
  globalThis.fetch=async(url)=>{assert.equal(url,'https://hh.ru/vacancy/123','public data succeeds without Actor or other providers');return new Response(html(job),{headers:{'content-type':'text/html'}});};
  const result=await fetchApifyDetail(env,{id:1,titleRevision:1,vacancyUrl:'https://hh.ru/vacancy/123'});assert.equal(result.snapshot.content_status,'valid');assert.equal(JSON.parse(result.snapshot.diagnostics).provider,'hh-public');assert.match(result.snapshot.markdown_raw,/Дата публикации: 2026-10-07/);assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,0);
  sqlite.exec('UPDATE apify_settings SET enabled=0');
  const independent=await fetchApifyDetail(env,{id:1,titleRevision:1,vacancyUrl:'https://hh.ru/vacancy/123'});assert.equal(independent.snapshot.content_status,'valid');assert.equal(sqlite.prepare('SELECT count(*) n FROM apify_runs').get().n,0,'public HH is independent of Apify');
  let publicRequests=0;globalThis.fetch=async url=>{assert.equal(url,'https://hh.ru/vacancy/123');publicRequests++;return new Response('captcha',{status:403})};await assert.rejects(fetchApifyDetail(env,{vacancyUrl:'https://hh.ru/vacancy/123'}),e=>e.code==='apify_disabled');assert.equal(publicRequests,1,'disabled fallback cannot call Apify');
 }finally{f.close();}
 console.log('HH public: exact JobPosting identity, expiry, text/date extraction, fixed URL/no credentials/no redirects/2MB bound, valid snapshot without Actor, disabled guard passed.');
}finally{globalThis.fetch=original;}
