import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch;
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const company=catalog.companies[0];company.vacancySources={hh:'https://hh.ru/employer/42'};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(company.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(company.name);sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1','apify_api_SYNTHETIC_PREFIX_12345'));
let starts=0,repeatWhole=false;const offsets=[];
globalThis.fetch=async(url,o)=>{const u=new URL(url);
 if(url.endsWith('/users/me'))return Response.json({data:{id:'account',plan:{id:'FREE',monthlyBasePriceUsd:0,monthlyUsageCreditsUsd:5}}});
 if(url.endsWith('/limits'))return Response.json({data:{monthlyUsageCycle:{startAt:'2026-10-01T00:00:00Z',endAt:'2026-11-01T00:00:00Z'},limits:{maxMonthlyUsageUsd:5},current:{monthlyUsageUsd:0}}});
 if(o.method==='POST'){starts++;assert.ok(starts<=4);return Response.json({data:{id:'run'+starts,defaultDatasetId:'data'+starts,status:'SUCCEEDED',usageTotalUsd:.01}});}
 if(!u.pathname.endsWith('/items'))return Response.json({data:{id:u.pathname.split('/').at(-1),itemCount:100,modifiedAt:'2000-01-01T00:00:00Z'}});
 const run=Number(u.pathname.split('/')[3].slice(4)),second=run%2===0,offset=Number(u.searchParams.get('offset'));offsets.push([run,offset]);return Response.json(Array.from({length:Math.max(0,Math.min(10,100-offset))},(_,i)=>{const n=offset+i,id=second&&n>=10&&!repeatWhole?n+91:n+1;return {title:'Manager '+id,vacancy_id:String(id),url:'https://hh.ru/vacancy/'+id,employer_id:'42'};}));
};
try{
 const s=(await api.readVacancyParsingSources(db)).find(x=>x.sourceId==='hh');await api.saveVacancyParsingSource(db,s.companyId,'hh',{enabled:true,sourceUrl:s.url,revision:s.revision,config:s.config});
 async function scan(){let r=await api.fetchVacancyListing(env,s.companyId,'hh',{userName:'Eugene'}),n=0;r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');while(r.paused&&n++<30){r=await api.resumeVacancyDiscoveryRun(env,r.runId,'Eugene');r=await api.saveVacancyDiscoveryRun(db,r.runId,'Eugene');assert.equal(r.saveErrors.length,0);}assert.ok(n<30);return r;}
 let r=await scan();assert.equal(starts,2,'uncertain pagination never launches a third Actor');assert.equal(r.errors[0].code,'apify_pagination_uncertain');assert.equal(r.status,'partial');assert.equal(r.coverage.datasetComplete,true);assert.equal(r.coverage.hhComplete,false);assert.equal(r.rawRows,200);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,190,'90 valid records after the repeated first10 are not discarded');assert.ok(offsets.some(([run,offset])=>run===2&&offset===90));assert.equal(sqlite.prepare('SELECT last_success_at FROM vacancy_source_parsing').get().last_success_at,null);
 repeatWhole=true;r=await scan();assert.equal(starts,4);assert.equal(r.errors[0].code,'apify_pagination_uncertain');assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,190,'whole repeated pages remain deduplicated');assert.ok(offsets.some(([run,offset])=>run===4&&offset===90));
 console.log('Apify repeated prefix: reads and saves all100 paid rows despite first10 repeats, keeps90 unseen tail records, whole-repeat dedupe, partial HH coverage and no additional Actor after uncertain pagination passed.');
}finally{globalThis.fetch=old;f.close();}
