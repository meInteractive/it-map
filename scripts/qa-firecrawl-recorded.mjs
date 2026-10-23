import assert from 'node:assert/strict';
import fs from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';

await prepareBackend();
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');
for(const object of buildBackupSchema(process.cwd()).objects)sqlite.exec(object.sql);
let queue=Promise.resolve();
const db={prepare(sql){let args=[];return{bind(...values){args=values;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}},async run(){const statement=sqlite.prepare(sql);return statement.columns().length?{results:statement.all(...args)}:{meta:statement.run(...args)}}}},batch(statements){const operation=queue.then(async()=>{sqlite.exec('BEGIN');try{const results=[];for(const statement of statements)results.push(await statement.run());sqlite.exec('COMMIT');return results}catch(error){sqlite.exec('ROLLBACK');throw error}});queue=operation.catch(()=>{});return operation}};
const catalog=JSON.parse(fs.readFileSync('src/data.json','utf8')),company=catalog.companies[0].name;
sqlite.prepare('INSERT OR REPLACE INTO app_catalog(id,data_json) VALUES(1,?)').run(JSON.stringify(catalog));
const api=await import('../.tests/backend.mjs?recorded-firecrawl-qa'),worker=api.default,base='https://roles.test',env={DB:db,SITE_PASSWORD:'test',NASTYA_PASSWORD:'test',SESSION_SECRET:'roles-secret'};
async function login(name){const response=await worker.fetch(new Request(base+'/auth',{method:'POST',body:new URLSearchParams({login:name,password:'test'})}),env);return response.headers.get('set-cookie').split(';')[0];}
const admin=await login('Eugene'),user=await login('Nastya');
const request=(path,method='GET',body,cookie=admin)=>worker.fetch(new Request(base+path,{method,headers:{cookie,origin:base,'content-type':'application/json'},...(body===undefined?{}:{body:JSON.stringify(body)})}),env);



import {extractVacancyListing} from '../server/services/vacancy-listing-extractor.js';
const rawDirectory=process.argv[2];if(!rawDirectory)throw new Error('Pass directory containing the four recorded firecrawl-*-qa.json responses. This script never calls a vendor.');
const sources=await api.readVacancyParsingSources(db),fixtures=[],report=[];
try{
 for(const [name,sourceId,file] of [['Selectel','companySite','selectel'],['Ozon Tech','habr','ozon-habr'],['Циан','companySite','cian'],['MIND Software','companySite','mindsw']]){
  const source=sources.find(s=>s.companyName===name&&s.sourceId===sourceId);assert.ok(source);const answer=JSON.parse(fs.readFileSync(rawDirectory+'/firecrawl-'+file+'-qa.json'));const x=extractVacancyListing(answer.data,{pageUrl:source.url,hosts:[new URL(source.url).hostname],config:source.config});
  const blocked=/captcha/i.test(answer.data.markdown);let created=0,known=0;
  if(!blocked&&x.ok){for(const job of x.jobs){const input={titleRaw:job.title,vacancyUrl:job.url,companyId:source.companyId,companyNameRaw:name,sourceId,sourceUrl:source.url,sourceNamespace:'company:'+source.companyId+':'+sourceId+':'+new URL(job.url).origin,externalJobId:job.externalJobId,acquisitionMethod:'firecrawl'};const first=await api.upsertVacancyCandidate(db,input);assert.equal(first.created,true);created++;const second=await api.upsertVacancyCandidate(db,input);assert.equal(second.created,false);assert.equal(second.id,first.id);assert.equal(second.candidate.titleRevision,1);known++;}}
  report.push({company:name,sourceId,url:source.url,loaded:answer.data.metadata.statusCode,proxy:answer.data.metadata.proxyUsed,credits:answer.data.metadata.creditsUsed,found:blocked?0:x.jobs.length,created,repeatKnown:known,outcome:blocked?'blocked':x.ok?'complete':'extraction'});
  const markdown=file==='selectel'?answer.data.markdown.slice(answer.data.markdown.indexOf('[BI /'),answer.data.markdown.indexOf('[Продажи')):answer.data.markdown;
  fixtures.push({company:name,pageUrl:source.url,adapter:source.config.adapter,expected:file==='selectel'?2:file==='ozon-habr'?1:0,data:{markdown,links:file==='selectel'?answer.data.links.filter(l=>/1951|1910/.test(l)):answer.data.links}});
 }
 assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_candidates').get().n,29);assert.equal(sqlite.prepare('SELECT count(*) n FROM company_vacancies').get().n,0);assert.equal(sqlite.prepare('SELECT count(*) n FROM vacancy_classification_attempts').get().n,0);assert.equal(sqlite.prepare('PRAGMA foreign_key_check').all().length,0);console.log(JSON.stringify({report,isolatedCandidates:29,main:0,classificationCalls:0},null,2));
 fs.mkdirSync('scripts/fixtures',{recursive:true});fs.writeFileSync('scripts/fixtures/firecrawl-listings.json',JSON.stringify({verifiedAt:'2026-10-14',transport:'v2 scrape basic markdown links; recorded public responses; no credentials or raw metadata retained',fixtures},null,2)+'\n');
}finally{sqlite.close();}
