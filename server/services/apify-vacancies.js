import {apifyError,hhInput} from '../integrations/apify.js';
import {candidateHash} from '../repositories/vacancy-candidates.js';
import {checkVacancyMarkdown} from './vacancy-fulltext.js';
import {executeApifyRun,apifyDatasetItems} from './apify-runs.js';
import {fetchPublicHhJob,hhHtmlText} from '../integrations/hh-public.js';
import {apifySettings} from '../repositories/apify-settings.js';
const descriptionOf=item=>typeof item.description_text==='string'&&item.description_text.trim()?item.description_text:typeof item.description_html==='string'?hhHtmlText(item.description_html):'';
export function apifyVacancy(item,{sourceUrl,detail=false}={}){
 if(!item||typeof item.title!=='string'||!item.title.trim()||Array.from(item.title).length>500||typeof item.url!=='string'||item.archived===true)throw apifyError('apify_data',422);
 let url;try{url=new URL(item.url);}catch{throw apifyError('apify_data',422);}
 const id=url.pathname.match(/^\/vacancy\/(\d+)\/?$/)?.[1];if(!id||url.protocol!=='https:'||!/(^|\.)hh\.ru$/.test(url.hostname)||url.username||url.password||url.port||item.vacancy_id!=null&&String(item.vacancy_id)!==id)throw apifyError('apify_data',422);
 const input=hhInput(sourceUrl,{detail}),source=new URL(input.startUrl),employer=source.searchParams.get('employer_id'),sourceId=source.pathname.match(/^\/vacancy\/(\d+)\/?$/)?.[1];
 if(employer&&String(item.employer_id)!==employer||sourceId&&sourceId!==id)throw apifyError('apify_data',422);
 const vacancyUrl='https://hh.ru/vacancy/'+id,description=descriptionOf(item);
 // Keep source values verbatim in Markdown. Never infer salary/date/roles here.
 const metadata=[['Компания',item.company],['Местоположение',item.location],['Зарплата',item.salary],['Опыт',item.experience],['Занятость',item.employment_type],['График',item.schedule],['Формат работы',Array.isArray(item.work_format)?item.work_format.join(', '):item.work_format],['Навыки',Array.isArray(item.skills)?item.skills.join(', '):item.skills],['Дата публикации',item.date_posted]].filter(([,v])=>typeof v==='string'&&v.trim()).map(([k,v])=>k+': '+v);
 return {title:item.title.trim(),url:vacancyUrl,externalJobId:id,sourceUrl,rawMarkdown:description?'# '+item.title.trim()+'\n\n'+metadata.join('\n\n')+'\n\n'+description:'',description,descriptionFormat:typeof item.description_text==='string'&&item.description_text.trim()?'text':'html'};
}
export async function saveApifySnapshot(db,candidateId,job,{profile,runId,provider='apify',buildId}={}){
 if(!job.rawMarkdown)return null;
 const validation=checkVacancyMarkdown(job.description,{sourceUrl:job.url,finalUrl:job.url,targetStatus:200});if(!validation.valid)return null;
 const hash=await candidateHash(job.rawMarkdown),latest=await db.prepare('SELECT * FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,rowid DESC LIMIT 1').bind(candidateId,job.url).first();
 if(latest?.content_hash===hash&&latest.content_status==='valid')return latest;
 const id=crypto.randomUUID(),time=new Date().toISOString(),large=new TextEncoder().encode(job.rawMarkdown).length>180000,diagnostics=JSON.stringify({...validation,provider,profile,runId,buildId,descriptionFormat:job.descriptionFormat});
 const statements=[db.prepare('INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,firecrawl_credits,content_status,diagnostics,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)').bind(id,candidateId,job.url,job.url,large?null:job.rawMarkdown,hash,time,profile==='starter'?'hobby':'free',null,'valid',diagnostics,time)];
 if(large){let n=0;for(let at=0;at<job.rawMarkdown.length;){let end=Math.min(job.rawMarkdown.length,at+48000);if(end<job.rawMarkdown.length&&/[\uD800-\uDBFF]/.test(job.rawMarkdown[end-1]))end--;statements.push(db.prepare('INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw) VALUES(?,?,?)').bind(id,n++,job.rawMarkdown.slice(at,end)));at=end;}}
 statements.push(db.prepare("INSERT INTO vacancy_content_fetches(id,candidate_id,snapshot_id,source_url,final_url,firecrawl_mode,credits,started_at,duration_ms,status,error_code,message) VALUES(?,?,?,?,?,?,NULL,?,0,'success',NULL,'')").bind(crypto.randomUUID(),candidateId,id,job.url,job.url,provider==='hh-public'?'hh-public':'apify-'+profile,Date.now()));
 await db.batch(statements);return db.prepare('SELECT * FROM vacancy_raw_snapshots WHERE id=?').bind(id).first();
}
export async function fetchApifyDetail(env,candidate,{old,refresh,signal}={}){
 const publicItem=await fetchPublicHhJob(candidate.vacancyUrl,{signal});
 if(publicItem){const job=apifyVacancy(publicItem,{sourceUrl:candidate.vacancyUrl,detail:true}),snapshot=await saveApifySnapshot(env.DB,candidate.id,job,{provider:'hh-public'});if(snapshot)return {snapshot,reused:false};}
 const settings=await apifySettings(env.DB);if(!settings.enabled)throw apifyError('apify_disabled',400);
 const input=hhInput(candidate.vacancyUrl,{detail:true}),key='detail:'+candidate.id+':'+candidate.titleRevision+':'+(refresh?old?.id||'empty':'missing')+':proxy-v1';
 const {row,token,pending}=await executeApifyRun(env,{operationKey:key,input,signal,includeRunMetadata:true});if(pending)throw apifyError('apify_running');
 const items=await apifyDatasetItems(token,row.dataset_id,0,{signal});if(items.length!==1)throw apifyError('apify_data',422);
 const item=items[0];let same=false;try{same=new URL(hhInput(item.url,{detail:true}).startUrl).pathname===new URL(input.startUrl).pathname&&(item.vacancy_id==null||String(item.vacancy_id)===new URL(input.startUrl).pathname.split('/')[2]);}catch{}
 if(same&&item.archived!==true&&(typeof item.title!=='string'||!item.title.trim()||!descriptionOf(item).trim()))throw apifyError('apify_incomplete',422);
 const job=apifyVacancy(items[0],{sourceUrl:candidate.vacancyUrl,detail:true}),snapshot=await saveApifySnapshot(env.DB,candidate.id,job,{profile:row.profile,runId:row.run_id,buildId:row.build_id});if(!snapshot)throw apifyError('apify_data',422);return {snapshot,reused:false};
}
