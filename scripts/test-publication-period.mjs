import assert from 'node:assert/strict';
import fs from 'node:fs';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor,nav} from './test-support.mjs';
const dates=await moduleFor('shared/publication-period.ts'),now=Date.parse('2026-10-21T12:00:00Z'),match=(day,period,at=now)=>dates.matchesPublicationPeriod({publishedAt:day},period,at);
assert.equal(match('2026-10-15','week'),true);assert.equal(match('2026-10-14','week'),false);assert.equal(match('2026-10-22','week'),false);
assert.equal(match('2026-09-21','month'),true);assert.equal(match('2026-09-20','month'),false);assert.equal(match('2026-08-21','twoMonths'),true);assert.equal(match('2026-08-20','twoMonths'),false);assert.equal(match('2026-08-20','archive'),true);assert.equal(match('2026-08-21','archive'),false);
assert.equal(match('1900-01-01','all'),true);assert.equal(match('2099-01-01','all'),true);assert.equal(dates.matchesPublicationPeriod({},'all',now),true);assert.equal(match('invalid','all'),true);
assert.equal(dates.publicationDate('2026-02-30'),null);assert.equal(dates.moscowPublicationDay(Date.parse('2026-10-20T21:30:00Z')).day,21);
assert.equal(match('2026-09-30','month',Date.parse('2026-10-31T12:00:00Z')),true);assert.equal(match('2026-09-29','month',Date.parse('2026-10-31T12:00:00Z')),false);
assert.equal(match('2024-02-29','month',Date.parse('2024-03-31T12:00:00Z')),true);assert.equal(match('2025-02-28','month',Date.parse('2025-03-31T12:00:00Z')),true);assert.equal(match('2025-12-01','month',Date.parse('2026-01-01T12:00:00Z')),true);
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=Object.fromEntries(['fetch','window','document','localStorage','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]])),oldNow=Date.now;let root;
Date.now=()=>now;
try{
 const catalog=JSON.parse(fs.readFileSync('src/data.json'));catalog.companies=[{...catalog.companies[0],name:'Period Employer',jobs:[]}];catalog.agencies=[];sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare("INSERT INTO company_crm(company_name) VALUES('Period Employer')").run();
 const days=['2026-10-21','2026-10-15','2026-10-14','2026-09-21','2026-09-20','2026-08-21','2026-08-20',null,'2026-02-30',null,'2026-10-22','2026-10-20'];
 const insert=sqlite.prepare("INSERT INTO company_vacancies(id,company_name,title,url,published_at,loaded_at,created_at,archived) VALUES(?,'Period Employer',?,?,?,?,?,?)");
 days.forEach((day,i)=>insert.run(i+1,'Period vacancy '+(i+1),'https://period.fixture/'+i,day,i===7?'2026-10-19':i===8?'2026-10-18':null,i===9?'2026-08-19 12:00:00':'2026-10-21 12:00:00',i===11?1:0));
 sqlite.prepare("UPDATE company_vacancies SET level='Senior' WHERE id=7").run();
 const worker=api.default,base='https://period.fixture',login=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:'Nastya',password:env.NASTYA_PASSWORD})}),env),cookie=login.headers.get('set-cookie').split(';')[0],read=path=>worker.fetch(new Request(base+path,{headers:{cookie}}),env),all=await (await read('/api/vacancies/list')).json();assert.equal(all.vacancies.length,12,'bare endpoint remains compatible');
 const snapshot=()=>JSON.stringify(sqlite.prepare('SELECT id,published_at,loaded_at,created_at,archived FROM company_vacancies ORDER BY id').all()),before=snapshot(),ids=data=>data.vacancies.map(v=>v.id).sort((a,b)=>a-b);
 for(const [period,expected] of Object.entries({week:[1,2,8,9,12],month:[1,2,3,4,8,9,12],twoMonths:[1,2,3,4,5,6,8,9,12],archive:[7,10],all:Array.from({length:12},(_,i)=>i+1)})){
  const response=await read('/api/vacancies/list?period='+period);assert.equal(response.status,200);assert.deepEqual(ids(await response.json()),expected,period);
 }
 assert.equal((await read('/api/vacancies/list?period=bad')).status,400);assert.equal((await worker.fetch(new Request(base+'/api/vacancies/list?period=archive'),env)).status,401);assert.equal(snapshot(),before,'age filter neither deletes nor sets archived flags');
 const {default:Vacancies}=await moduleFor('frontend/features/vacancies/Vacancies.tsx'),{defaultFilters}=await moduleFor('frontend/features/companies/Filters.tsx'),{defaultVacancyFilters,publicationLabel}=await moduleFor('frontend/features/vacancies/model.ts');assert.equal(publicationLabel(all.vacancies.find(v=>v.id===10)),'~ 19.08.2026');
 globalThis.IS_REACT_ACT_ENVIRONMENT=true;globalThis.window={addEventListener(){},removeEventListener(){}};globalThis.document={addEventListener(){},removeEventListener(){},visibilityState:'visible'};const calls=[];globalThis.fetch=async(url,options={})=>{assert.ok(!options.method||options.method==='GET');calls.push(url);return read(url);};
 const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0));});await act(async()=>{root=create(h(Vacancies,{nav,admin:false,filters:defaultFilters,vacancyFilters:defaultVacancyFilters,query:'',onCount(){}}));});await flush();
 const select=()=>root.root.findByProps({'aria-label':'Дата публикации вакансии'}),titles=()=>root.root.findAllByProps({className:'vacancy-title-button'}).map(n=>n.children.join('')).sort();
 assert.equal(select().props.value,'week');assert.deepEqual(select().findAllByType('option').map(o=>o.children.join('')),['Неделя','Месяц','2 месяца','Архив','Все']);assert.deepEqual(titles(),[1,2,8,9,12].map(i=>'Period vacancy '+i).sort());
 await act(async()=>select().props.onChange({target:{value:'month'}}));await flush();assert.ok(titles().includes('Period vacancy 4'));assert.ok(!titles().includes('Period vacancy 5'));
 await act(async()=>select().props.onChange({target:{value:'archive'}}));await flush();assert.deepEqual(titles(),['Period vacancy 10','Period vacancy 7']);assert.equal(snapshot(),before);assert.ok(calls.every(url=>/^\/api\/vacancies\/list\?period=/.test(url)));
 await act(async()=>select().props.onChange({target:{value:'all'}}));await flush();assert.deepEqual(titles(),all.vacancies.map(v=>v.title).sort());
 const filteredProps={nav,admin:false,filters:defaultFilters,vacancyFilters:{...defaultVacancyFilters,levels:['Senior']},query:'Period vacancy 7',onCount(){}};
 await act(async()=>root.update(h(Vacancies,filteredProps)));await flush();
 await act(async()=>select().props.onChange({target:{value:'archive'}}));await flush();
 await act(async()=>select().props.onChange({target:{value:'all'}}));await flush();assert.deepEqual(titles(),['Period vacancy 7'],'All preserves search and level filters');assert.equal(snapshot(),before);
 console.log('Publication period: Moscow 7-day window, rolling calendar months/end-of-month/leap/year boundaries, disjoint age archive, approximate dates, compatible authenticated HTTP and real UI default/change/no deletion passed.');
}finally{if(root)await act(async()=>root.unmount());Date.now=oldNow;Object.assign(globalThis,old);f.close();}
