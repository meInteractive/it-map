import {fetchApifyDetail} from './apify-vacancies.js';
import {candidateDb,candidateHash,candidateId,getVacancyCandidate,canonicalCandidateUrl,candidateError} from '../repositories/vacancy-candidates.js';
import {withDetailLease} from './vacancy-detail-leases.js';
import {createFirecrawlUsageSession,firecrawlScrape,preflightListingUrl,safeListingUrl} from '../integrations/firecrawl.js';
import {firecrawlSettings} from '../repositories/firecrawl-settings.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {generationBoundDb,boundGeneration} from '../repositories/generation-bound-db.js';
import {rabotaListingHosts} from './rabota-listing.js';
import {rabotaBodyPresent,rabotaDetailMarkdown,RABOTA_MISSING_BODY,incompleteRabotaBody} from './rabota-detail.js';
export function checkVacancyMarkdown(markdown,{sourceUrl,finalUrl,targetStatus,listingUrl}={}){
 const errors=[];if(typeof markdown!=='string'||markdown.trim().length<250)errors.push('Описание слишком короткое или отсутствует.');
 if(!(targetStatus>=200&&targetStatus<300))errors.push('Не подтверждён успешный статус страницы.');
 const text=(markdown||'').slice(0,12000),head=text.slice(0,3000);
 if(/captcha|verify.{0,30}human|access denied|вакансия.{0,30}(?:закрыта|удалена|в архиве)|job.{0,30}(?:closed|removed|no longer available)|page not found|страница не найдена|(?:^|\n)\s*#?\s*(?:вход|авторизация|sign in|log in)(?:\n|$)/i.test(head))errors.push('Страница закрыта, требует входа или не содержит действующую вакансию.');
 const signals=[/обязанност|(?:чем|что|вам) предстоит|вы будете|responsibilit|what you.ll do|задачи/i,/требован|ожидаем от|мы жд[её]м|навык|опыт|requirements|qualifications/i,/услови|предлагаем|benefits|what we offer|зарплат|salary/i].filter(r=>r.test(text)).length;
 if(signals<2)errors.push('Не подтверждено содержательное описание конкретной вакансии.');
 try{if(sourceUrl&&/(^|\.)rabota\.ru$/.test(new URL(sourceUrl).hostname)&&!rabotaBodyPresent(markdown))errors.push(RABOTA_MISSING_BODY);}catch{/* The URL validation below reports malformed input. */}
 try{const a=new URL(sourceUrl),b=new URL(finalUrl);if((listingUrl&&canonicalCandidateUrl(finalUrl)===canonicalCandidateUrl(listingUrl)&&canonicalCandidateUrl(sourceUrl)!==canonicalCandidateUrl(listingUrl))||(b.pathname!==a.pathname&&/^\/(?:careers?|jobs?|vacanc(?:y|ies)|вакансии)?\/?$/i.test(b.pathname)))errors.push('Вместо карточки получен общий список вакансий.');}catch{errors.push('Некорректный конечный URL.');}
 if(/^\s*#\s*(?:вакансии|открытые позиции|open positions|job openings)\s*$/im.test(head)&&((text.match(/^#{1,3} /gm)||[]).length>3))errors.push('Получен список нескольких вакансий.');
 return {valid:!errors.length,errors};
}
export async function currentRawSnapshot(db,candidate){
 const row=await db.prepare('SELECT * FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,rowid DESC LIMIT 1').bind(candidate.id,candidate.vacancyUrl).first();
 if(row?.content_status!=='valid')return null;
 if(candidate.sourceId==='rabota'&&!rabotaBodyPresent(await snapshotMarkdown(db,row)))return null;
 return row;
}
export async function snapshotMarkdown(db,snapshot){
 if(snapshot.markdown_raw!==null)return snapshot.markdown_raw;
 return ((await db.prepare('SELECT markdown_raw FROM vacancy_raw_snapshot_parts WHERE snapshot_id=? ORDER BY part_index').bind(snapshot.id).all()).results||[]).map(r=>r.markdown_raw).join('');
}
export async function eligibleDetailCandidate(db,id,{allowImported=false}={}){
 const detail=await getVacancyCandidate(db,candidateId(id));if(!detail)throw candidateError('Первичная вакансия не найдена.',404);
 if(!(allowImported?detail.currentResults.some(r=>r.decision==='match'):detail.candidate.processingStatus==='accepted'&&detail.candidate.mainVacancyId==null))throw Object.assign(candidateError('Нужен актуальный match активной корневой роли; импортированные записи не обрабатываются.',409),{code:'not_accepted'});
 safeListingUrl(detail.candidate.vacancyUrl);return detail;
}
export async function fetchCandidateMarkdown(env,id,{refresh=false,confirmed=false,signal,allowImported=false}={}){
 if(refresh&&confirmed!==true)throw candidateError('Подтвердите повторную загрузку с расходом провайдера.');const db=await candidateDb(env.DB);
 return withDetailLease(db,'candidate:'+candidateId(id),async owned=>{
  const detail=await eligibleDetailCandidate(owned,id,{allowImported}),c=detail.candidate,old=await currentRawSnapshot(owned,c);
  if(old&&!refresh)return {snapshot:old,reused:true};
  if(c.sourceId==='hh'){
   let scoped=owned;
   if(c.companyId){const source=await readVacancyParsingSource(owned,c.companyId,'hh');if(!source.enabled)throw Object.assign(new Error('Источник HH выключен.'),{code:'source_scope',status:409});scoped=generationBoundDb(owned,boundGeneration(owned),{sql:'EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE company_id=? AND source_id=\'hh\' AND enabled=1 AND revision=? AND source_url=?)',bindings:[c.companyId,source.revision,source.url],failure:()=>Object.assign(new Error('Источник HH изменился.'),{code:'source_scope',status:409})});}
   return fetchApifyDetail({...env,DB:scoped},c,{old,refresh,signal});
  }
  const s=await firecrawlSettings(owned),started=Date.now();let response,raw,validation,snapshotId=null;
  try{
   if(!s.enabled)throw Object.assign(new Error('Firecrawl выключен. Сохранённые тексты можно структурировать без него.'),{code:'disabled',status:400});
   const session=await createFirecrawlUsageSession({...env,DB:owned},{signal}),host=new URL(c.vacancyUrl).hostname,hosts=c.sourceId==='rabota'&&/(^|\.)rabota\.ru$/.test(host)?rabotaListingHosts(host):[host];
   const final=await preflightListingUrl(c.vacancyUrl,hosts,{signal});
   const rabota=c.sourceId==='rabota';
   response=await firecrawlScrape(session,final,{hosts,markdownOnly:!rabota,html:rabota,onlyMainContent:!rabota,timeoutMs:s.timeout_ms,proxy:'basic',signal,budget:{remaining:1}});
   raw=rabota?rabotaDetailMarkdown(response.data,response.finalUrl):typeof response.data.markdown==='string'?response.data.markdown:'';
   validation=checkVacancyMarkdown(raw,{sourceUrl:c.vacancyUrl,finalUrl:response.finalUrl,targetStatus:response.targetStatus,listingUrl:c.sourceUrl});
   const failureCode=c.sourceId==='rabota'&&incompleteRabotaBody(validation)?'rabota_incomplete_body':'invalid_content';
   // Preserve received content as history even if the current root settings changed.
   // Generation/owner guards prevent attaching data after a restore.
   const hash=await candidateHash(raw),latest=await owned.prepare('SELECT * FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,rowid DESC LIMIT 1').bind(c.id,c.vacancyUrl).first();
   snapshotId=latest?.content_hash===hash&&latest.content_status===(validation.valid?'valid':'invalid')?latest.id:crypto.randomUUID();
   if(snapshotId!==latest?.id){
    const time=new Date().toISOString(),large=raw.length>200000,statements=[owned.prepare('INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,firecrawl_credits,content_status,diagnostics,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)').bind(snapshotId,c.id,c.vacancyUrl,response.finalUrl,large?null:raw,hash,time,s.mode,response.creditsReported,validation.valid?'valid':'invalid',JSON.stringify(validation),time)];
    if(large){let seq=0;for(let at=0;at<raw.length;){let end=Math.min(raw.length,at+128000);if(end<raw.length&&/[\uD800-\uDBFF]/.test(raw[end-1]))end--;statements.push(owned.prepare('INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw) VALUES(?,?,?)').bind(snapshotId,seq++,raw.slice(at,end)));at=end;}}
    await owned.batch(statements);
   }
   await owned.prepare('INSERT INTO vacancy_content_fetches(id,candidate_id,snapshot_id,source_url,final_url,firecrawl_mode,credits,started_at,duration_ms,status,error_code,message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)').bind(crypto.randomUUID(),c.id,snapshotId,c.vacancyUrl,response.finalUrl,s.mode,response.creditsReported,started,Date.now()-started,validation.valid?'success':'error',validation.valid?null:failureCode,validation.errors.join(' ')).run();
   if(!validation.valid)throw Object.assign(new Error(validation.errors.join(' ')),{code:failureCode,status:422,recorded:true});
   return {snapshot:await owned.prepare('SELECT * FROM vacancy_raw_snapshots WHERE id=?').bind(snapshotId).first(),reused:snapshotId===old?.id};
  }catch(e){
   if(!e.recorded&&e.code!=='restored'&&e.code!=='detail_changed')await owned.prepare('INSERT INTO vacancy_content_fetches(id,candidate_id,source_url,final_url,firecrawl_mode,credits,started_at,duration_ms,status,error_code,message) VALUES(?,?,?,?,?,?,?,?,?,?,?)').bind(crypto.randomUUID(),c.id,c.vacancyUrl,e.finalUrl||null,s.mode,e.creditsReported??null,started,Date.now()-started,'error',e.code||'storage',e.status?e.message:'Не удалось получить полное описание.').run();throw e;
  }
 });
}
