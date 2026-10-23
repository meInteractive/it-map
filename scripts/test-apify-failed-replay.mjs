import assert from 'node:assert/strict';
import fs from 'node:fs';
import {schedulerFixture} from './scheduler-test-support.mjs';
import {moduleFor} from './test-support.mjs';
const f=await schedulerFixture(),{api,db,env,sqlite}=f,old=fetch;
const {replayApifyListing,inspectApifyListingReplays}=await moduleFor('server/services/apify-listing-replay.js');
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8'));catalog.companies=catalog.companies.slice(0,1);catalog.agencies=[];const company=catalog.companies[0];company.vacancySources={hh:'https://hh.ru/employer/42'};
sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));sqlite.prepare('INSERT INTO company_crm(company_name) VALUES(?)').run(company.name);sqlite.prepare("INSERT INTO company_user_state(user_name,company_name,favorite) VALUES('Eugene',?,1)").run(company.name);
const token='apify_api_SYNTHETIC_FAILED_REPLAY_12345';sqlite.prepare('INSERT INTO apify_settings(id,enabled,free_key_encrypted) VALUES(1,1,?)').run(await api.encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token));
const source=(await api.readVacancyParsingSources(db)).find(x=>x.sourceId==='hh');await api.saveVacancyParsingSource(db,source.companyId,'hh',{enabled:true,sourceUrl:source.url,revision:source.revision,config:source.config});
const s=await api.schedulerSettings(db);await api.saveSchedulerSettings(db,{enabled:true,sourceIds:['hh'],dayBudgetUsd:1,monthBudgetUsd:5,revision:s.revision},'Eugene');
const generation=sqlite.prepare('SELECT generation FROM app_runtime_generation').get().generation,ledger='33333333-3333-4333-8333-333333333333',parent='11111111-1111-4111-8111-111111111111';
sqlite.prepare("INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,'Eugene',?,?,'hh',?,1,0,?,1,2)").run(parent,generation,source.companyId,source.url,JSON.stringify({status:'error',paused:null,errors:[{code:'apify_failed'}]}));
sqlite.prepare("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES(?,?,?,'free','account',?,'s','e',.25,.05,'failed','paidRun','paidData',1,1)").run(ledger,generation,'listing:'+parent+':1',await api.candidateHash(token));
await api.upsertVacancyCandidate(db,{titleRaw:'Manual title',vacancyUrl:'https://hh.ru/vacancy/1',companyId:source.companyId,sourceId:'hh',sourceNamespace:'manual',externalJobId:'1'});
const prior=sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=1').get(),identity=sqlite.prepare('SELECT * FROM vacancy_candidate_identities WHERE candidate_id=1 ORDER BY identity_key').all(),ledgerBefore=sqlite.prepare('SELECT * FROM apify_runs').get(),sourceBefore=sqlite.prepare('SELECT * FROM vacancy_source_parsing').get();
let status='TIMED-OUT',account='account',dataset='paidData',race=false,reads=0;
const rows=Array.from({length:27},(_,i)=>({title:'Manager '+(i+1),vacancy_id:String(i+1),url:'https://hh.ru/vacancy/'+(i+1),employer_id:'42'}));rows[1]={};rows[7].employer_id='999';
globalThis.fetch=async(url,o)=>{reads++;assert.equal(o.method,'GET');assert.equal(o.headers.authorization,'Bearer '+token);const u=new URL(url);assert.equal(u.hostname,'api.apify.com');
 if(u.pathname==='/v2/actor-runs/paidRun')return Response.json({data:{id:'paidRun',defaultDatasetId:dataset,userId:account,status}});
 assert.ok(u.pathname.startsWith('/v2/datasets/paidData'));if(!u.pathname.endsWith('/items')){if(race)sqlite.prepare("UPDATE apify_runs SET dataset_id='other'").run();return Response.json({data:{id:'paidData',itemCount:27,modifiedAt:'2000-01-01T00:00:00Z'}});}
 return Response.json(rows.slice(Number(u.searchParams.get('offset')),Number(u.searchParams.get('offset'))+10));
};
try{
 const inspection=await inspectApifyListingReplays(env);assert.equal(inspection.candidates[0].sourceRunState,'failed');assert.equal(reads,0);
 for(const value of ['RUNNING','SUCCEEDED','ABORTING']){status=value;await assert.rejects(replayApifyListing(env,ledger),e=>e.code==='apify_changed');}status='TIMED-OUT';
 account='other';await assert.rejects(replayApifyListing(env,ledger),e=>e.code==='apify_changed');account='account';dataset='other';await assert.rejects(replayApifyListing(env,ledger),e=>e.code==='apify_changed');dataset='paidData';
 race=true;await assert.rejects(replayApifyListing(env,ledger),e=>e.code==='apify_changed');race=false;sqlite.prepare("UPDATE apify_runs SET dataset_id='paidData'").run();assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,1,'ledger mutation fences dataset writes');
 let r=await replayApifyListing(env,ledger);assert.equal(r.processed,20);assert.equal(r.createdCandidates,17);r=await replayApifyListing(env,ledger);assert.equal(r.complete,true);assert.equal(r.rawRows,27);assert.equal(r.createdCandidates,24);assert.equal(r.existingRows,1);assert.equal(r.quarantinedCount,2);assert.equal(r.status,'partial');assert.equal(r.sourceRunState,'failed');assert.equal(r.providerStatus,'TIMED-OUT');assert.equal(r.coverage.hhComplete,false);assert.equal(r.actorStarts,0);assert.equal(r.modelCalls,0);
 assert.deepEqual(sqlite.prepare('SELECT * FROM vacancy_candidates WHERE id=1').get(),prior);assert.deepEqual(sqlite.prepare('SELECT * FROM vacancy_candidate_identities WHERE candidate_id=1 ORDER BY identity_key').all(),identity);assert.deepEqual(sqlite.prepare('SELECT * FROM apify_runs').get(),ledgerBefore);assert.deepEqual(sqlite.prepare('SELECT * FROM vacancy_source_parsing').get(),sourceBefore);assert.equal(sqlite.prepare("SELECT count(*) n FROM scheduler_jobs WHERE type='classify_title'").get().n,24);assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);
 const doneReads=reads;await replayApifyListing(env,ledger);assert.equal(reads,doneReads,'completed failed dataset has no further network reads');
 sqlite.prepare('UPDATE apify_runs SET charged_usd=NULL').run();await assert.rejects(replayApifyListing(env,ledger),e=>e.code==='apify_changed');assert.equal(reads,doneReads,'unknown billing is not accepted');
 console.log('Failed paid replay: explicit terminal/account/dataset proof, ledger race fence, bounded27raw/24missing, manual fields/aliases/billing/schedule unchanged, no POST/model/import and completed replay without GET passed.');
}finally{globalThis.fetch=old;f.close();}
