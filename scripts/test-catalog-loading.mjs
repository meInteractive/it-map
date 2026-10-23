import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {DatabaseSync} from 'node:sqlite';
import {gzipSync} from 'node:zlib';
const worker=(await import('../.tests/backend.mjs')).default;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const file of fs.readdirSync('drizzle').filter(f=>f.endsWith('.sql')).sort())sqlite.exec(fs.readFileSync('drizzle/'+file,'utf8'));
let queries=[];
const db={prepare(sql){let args=[];return {bind(...values){args=values;return this},async first(){queries.push(sql);return sqlite.prepare(sql).get(...args)??null},async all(){queries.push(sql);return {results:sqlite.prepare(sql).all(...args)}},async run(){queries.push(sql);const p=sqlite.prepare(sql);return p.columns().length?{results:p.all(...args)}:{meta:p.run(...args)}}}},async batch(items){sqlite.exec('BEGIN');try{const out=[];for(const item of items)out.push(await item.run());sqlite.exec('COMMIT');return out}catch(e){sqlite.exec('ROLLBACK');throw e}}};
const env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'test-secret'};
const base='https://catalog.test';
const cookies={};for(const name of ['Eugene','Nastya']){const r=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);cookies[name]=r.headers.get('set-cookie').split(';')[0]}
const req=(path,user='Eugene',method='GET')=>worker.fetch(new Request(base+path,{method,headers:{cookie:cookies[user]||''}}),env);
const original=JSON.parse(fs.readFileSync('src/data.json','utf8'));
const catalog={...original,companies:Array.from({length:300},(_,i)=>({...original.companies[0],name:'Fixture '+String(i).padStart(3,'0'),jobs:[],wlb:'PRIVATE_WLB',risk:'PRIVATE_RISK',vacancySources:{companySite:'https://fixture.invalid/private'},reviews:{plus:'PRIVATE_REVIEW'},offices:[{address:'Адрес',metro:'Тест',lat:55.75,lon:37.62,source:'PRIVATE_SOURCE',status:'PRIVATE_STATUS'}]})),agencies:[]};
try{
 sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
 sqlite.exec("INSERT INTO company_crm(company_name) VALUES('Fixture 000'),('Fixture 001'),('Fixture 002'); INSERT INTO company_user_state(user_name,company_name,favorite,hidden) VALUES('Eugene','Fixture 000',1,1),('Nastya','Fixture 001',1,1); INSERT INTO company_vacancies(id,company_name,url,title,real_company_name) VALUES(900001,'Fixture 001','https://fixture.invalid/job','Title','Fixture 002'); INSERT INTO company_applications(id,company_name,user_name,status,vacancy_id) VALUES(1,'Fixture 000','Eugene','Офер',NULL),(2,'Fixture 001','Eugene','Офер',NULL),(3,'Fixture 001','Eugene','Отказ',900001),(4,'Fixture 002','Nastya','Откликнулся',NULL)");
 queries=[];assert.equal(await req('/'),null);const html=await shell();assert.equal(queries.length,0,'Initial shell must not read application data');assert.ok(!/PRIVATE_WLB|Fixture 000|const DATA=|COMPANY_NAMES_WITH_VACANCIES/.test(html));
 queries=[];const response=await req('/api/companies/list');assert.equal(response.status,200);const raw=await response.text(),rows=JSON.parse(raw).companies;assert.equal(rows.length,300);assert.equal(queries.length,6,'Constant query count: catalog + identities + 4 batched selects');assert.ok(!queries.some(q=>/SELECT \* FROM company_(vacancies|applications)/i.test(q)));
 const expected=['id','name','rank','entityType','favorite','hidden','hasVacancies','hasApplications','applicationStatus','offices','middle','senior','lead','head','deferment','rating','wlbScore','hasVacancySource'].sort();assert.deepEqual(Object.keys(rows[0]).sort(),expected);assert.deepEqual(Object.keys(rows[0].offices[0]).sort(),['address','metro']);assert.ok(!raw.includes('PRIVATE_'));const find=name=>rows.find(c=>c.name===name);
 assert.equal(find('Fixture 000').favorite,true);assert.equal(find('Fixture 000').hidden,true);assert.equal(find('Fixture 000').hasApplications,true);assert.equal(find('Fixture 000').hasVacancies,false);
 assert.equal(find('Fixture 001').hasVacancies,true);assert.equal(find('Fixture 001').applicationStatus,'Отказ','Vacancy-linked application takes precedence');assert.equal(find('Fixture 002').hasVacancies,true,'Real employer link counts');assert.equal(find('Fixture 002').hasApplications,false,'Applications are personal');
 const other=(await (await req('/api/companies/list','Nastya')).json()).companies;assert.equal(other.find(c=>c.name==='Fixture 000').hidden,false);assert.equal(other.find(c=>c.name==='Fixture 001').favorite,true);
 const map=(await (await req('/api/companies/map')).json()).companies;assert.equal(map[0].offices[0].lat,55.75);assert.ok(!('middle' in map[0]));
 const detail=(await (await req('/api/companies/'+find('Fixture 000').id)).json()).company;assert.equal(detail.wlb,'PRIVATE_WLB');assert.ok(!('jobs' in detail));assert.equal((await req('/api/companies/999999')).status,404);assert.equal((await req('/api/companies/list','Eugene','POST')).status,405);assert.equal((await req('/api/companies/list','')).status,401);
 // Same local filters and cache contract, now exercised through typed React modules.
 const {matchesEntity,defaultFilters}=await moduleFor('frontend/features/companies/Filters.tsx');
 for(let mask=0;mask<16;mask++){const f={...defaultFilters,favoriteOnly:!!(mask&1),showHidden:!!(mask&2),vacanciesOnly:!!(mask&4),applicationsOnly:!!(mask&8)};assert.deepEqual(rows.filter(c=>matchesEntity(c,f)).map(c=>c.name),rows.filter(c=>(!f.favoriteOnly||c.favorite)&&(f.showHidden||!c.hidden)&&(!f.vacanciesOnly||c.hasVacancies)&&(!f.applicationsOnly||c.hasApplications)).map(c=>c.name));}
 const keys=['rankTop','rankBottom','rank101to150','rank151to200','rank201to250','rank251to300','rankBeyond300','rankUnranked'];
 for(let i=0;i<keys.length;i++){const f={...defaultFilters,...Object.fromEntries(keys.map((k,j)=>[k,i===j]))};assert.deepEqual([1,51,101,151,201,251,301,null].map(rank=>matchesEntity({rank},f)),keys.map((_,j)=>i===j));}
 await (await import('./test-react.mjs')).testCache();
 await (await import('./test-react.mjs')).testInitialLoading();
 // Measure the existing seed catalog separately from synthetic long text.
 sqlite.prepare('UPDATE app_catalog SET data_json=?').run(JSON.stringify(original));const actual=await (await req('/api/companies/list')).text();console.log(JSON.stringify({test:'catalog-loading',companies:JSON.parse(actual).companies.length,jsonBytes:Buffer.byteLength(actual),gzipBytes:gzipSync(actual).length,synthetic300Bytes:Buffer.byteLength(raw),listSqlStatements:6}));
 console.log('Catalog verified: empty shell, strict projections, constant SQL count, ownership, real-employer vacancies, general applications, 16 filter combinations, list/detail cache and separate map.');
}finally{sqlite.close()}
