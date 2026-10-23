import {operationErrorCode} from './operation-error.js';
import {candidateDb,candidateId,candidateHash,getVacancyCandidate,candidateError} from '../repositories/vacancy-candidates.js';
import {readDetailSettings,detailModel} from '../repositories/vacancy-detail-settings.js';
import {withDetailLease} from './vacancy-detail-leases.js';
import {currentRawSnapshot,snapshotMarkdown,eligibleDetailCandidate} from './vacancy-fulltext.js';
import {EXTRACTION_SCHEMA,EXTRACTION_SCHEMA_VERSION,EXTRACTION_INPUT_LIMIT,normalizeVacancyExtraction} from './vacancy-extraction-schema.js';
import {runPromptAI} from '../integrations/openai.js';
import {openaiPricing} from '../integrations/openai-pricing.js';
const numeric=v=>typeof v==='number'&&Number.isFinite(v)&&v>=0?v:null;
export async function extractionContext(db){const s=await readDetailSettings(db),config=detailModel(s);return {prompt:s.markdown,config,promptVersion:await candidateHash(s.markdown),configHash:await candidateHash(JSON.stringify([config.provider,config.openaiModel,config.reasoningEffort,config.openrouterModel])),schemaVersion:EXTRACTION_SCHEMA_VERSION};}
export async function currentExtraction(db,candidate,snapshot,context){
 if(!snapshot)return null;const x=context||await extractionContext(db);
 return db.prepare("SELECT * FROM vacancy_extractions WHERE candidate_id=? AND snapshot_id=? AND config_hash=? AND prompt_version=? AND schema_version=? AND status='success' ORDER BY processed_at DESC,rowid DESC LIMIT 1").bind(candidate.id,snapshot.id,x.configHash,x.promptVersion,x.schemaVersion).first();
}
export async function withLlmSlot(db,work){for(const slot of ['llm:1','llm:2']){let entered=false;try{return await withDetailLease(db,slot,owned=>{entered=true;return work(owned)});}catch(e){if(e.code!=='detail_busy'||entered)throw e;}}throw Object.assign(candidateError('Два запроса структурирования уже выполняются.',409),{code:'llm_busy'});}
export function extractionUsage(result,provider){
 const u=result?.usage||{},input=numeric(u.input_tokens??u.prompt_tokens),output=numeric(u.output_tokens??u.completion_tokens);let cost=numeric(u.cost),estimated=false;
 const rate=provider==='openai'?openaiPricing(result?.model):null;
 // Cached-token rates are not available in the shared tariff table. Keep cost
 // unknown rather than present the uncached upper bound as an actual charge.
 if(cost===null&&rate&&input!==null&&output!==null&&!(u.input_tokens_details?.cached_tokens>0)){cost=(input*rate.inputUsdPerMillion+output*rate.outputUsdPerMillion)/1e6;estimated=true;}
 return {input,output,cost,estimated};
}
export async function extractCandidateMarkdown(env,id,{signal,allowImported=false}={}){
 const db=await candidateDb(env.DB);return withDetailLease(db,'candidate:'+candidateId(id),async owned=>{
  const detail=await eligibleDetailCandidate(owned,id,{allowImported}),c=detail.candidate,snapshot=await currentRawSnapshot(owned,c);
  if(!snapshot)throw Object.assign(candidateError('Нет проверенного полного текста. Сначала загрузите Markdown.',409),{code:'missing_markdown'});
  const x=await extractionContext(owned),cached=await currentExtraction(owned,c,snapshot,x);if(cached)return {extraction:cached,reused:true};
  const raw=await snapshotMarkdown(owned,snapshot),started=Date.now();let result,usage={input:null,output:null,cost:null,estimated:false};
  try{
   if(raw.length>EXTRACTION_INPUT_LIMIT)throw Object.assign(candidateError('Исходный Markdown сохранён целиком, но превышает безопасный лимит 120 000 символов для запроса LLM. Структурирование ожидает обработки без усечения.',422),{code:'input_too_large'});
   if(!x.prompt.trim())throw Object.assign(candidateError('Заполните промт «Структурирование вакансии».',409),{code:'empty_prompt'});
   result=await withLlmSlot(owned,slotDb=>runPromptAI({...env,DB:slotDb},{task:'vacancy_extraction',signal,maxAttempts:1,maxTokens:32768,validateContent:content=>{normalizeVacancyExtraction(content,raw);return true;},responseFormat:{type:'json_schema',json_schema:{name:'vacancy_details',strict:true,schema:EXTRACTION_SCHEMA}},messages:[{role:'system',content:x.prompt+'\nСерверная схема:\n'+JSON.stringify(EXTRACTION_SCHEMA)},{role:'user',content:JSON.stringify({source:{title:c.titleRaw,company:c.companyName||c.companyNameRaw,url:c.vacancyUrl},markdown:raw})}]},x.config));
   usage=extractionUsage(result,x.config.provider);const {data,corrections}=normalizeVacancyExtraction(result.message.content,raw);
   const diagnostic={sanitizedFields:corrections,costEstimated:usage.estimated,sourceCompany:c.companyName||c.companyNameRaw,extractedEmployer:data.employer_name,employerMismatch:!!(c.companyName||c.companyNameRaw)&&!!data.employer_name&&(c.companyName||c.companyNameRaw).toLowerCase()!==data.employer_name.toLowerCase(),sourceUrl:c.vacancyUrl};
   const extractionId=crypto.randomUUID();await owned.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,extracted_data,processed_at,duration_ms,input_tokens,output_tokens,cost_usd,status,diagnostics) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'success',?)").bind(extractionId,c.id,snapshot.id,x.config.provider,result.model,x.configHash,x.promptVersion,x.schemaVersion,JSON.stringify(data),new Date().toISOString(),Date.now()-started,usage.input,usage.output,usage.cost,JSON.stringify(diagnostic)).run();
   return {extraction:await owned.prepare('SELECT * FROM vacancy_extractions WHERE id=?').bind(extractionId).first(),reused:false};
  }catch(e){
   if(!['restored','detail_changed','llm_busy'].includes(e.code))await owned.prepare("INSERT INTO vacancy_extractions(id,candidate_id,snapshot_id,provider,model_id,config_hash,prompt_version,schema_version,processed_at,duration_ms,input_tokens,output_tokens,cost_usd,status,error_code,diagnostics) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'error',?,?)").bind(crypto.randomUUID(),c.id,snapshot.id,x.config.provider,result?.model??null,x.configHash,x.promptVersion,x.schemaVersion,new Date().toISOString(),Date.now()-started,usage.input,usage.output,usage.cost,operationErrorCode(e),JSON.stringify({message:e.status?e.message:'Запрос прерван; результат и стоимость неизвестны.',costEstimated:usage.estimated})).run();throw e;
  }
 });
}
export async function vacancyDetailState(db,id,{includeMarkdown=false,context}={}){
 db=await candidateDb(db);const detail=await getVacancyCandidate(db,id);if(!detail)throw candidateError('Первичная вакансия не найдена.',404);const c=detail.candidate;
 const latest=await db.prepare('SELECT * FROM vacancy_raw_snapshots WHERE candidate_id=? AND source_url=? ORDER BY created_at DESC,rowid DESC LIMIT 1').bind(c.id,c.vacancyUrl).first(),raw=latest?.content_status==='valid'?latest:null,x=context||await extractionContext(db),extraction=await currentExtraction(db,c,raw,x);
 const fetch=await db.prepare('SELECT * FROM vacancy_content_fetches WHERE candidate_id=? AND source_url=? ORDER BY started_at DESC,rowid DESC LIMIT 1').bind(c.id,c.vacancyUrl).first();
 const llm=raw?await db.prepare('SELECT * FROM vacancy_extractions WHERE candidate_id=? AND snapshot_id=? AND config_hash=? AND prompt_version=? AND schema_version=? ORDER BY processed_at DESC,rowid DESC LIMIT 1').bind(c.id,raw.id,x.configHash,x.promptVersion,x.schemaVersion).first():null;
 let validUrl=false;try{const {safeListingUrl}=await import('../integrations/firecrawl.js');safeListingUrl(c.vacancyUrl);validUrl=true;}catch{}
 const accepted=c.processingStatus==='accepted'&&c.mainVacancyId==null,eligible=accepted&&validUrl;
 const state={candidate:c,currentResults:detail.currentResults,accepted,eligible,validUrl,rawStatus:raw?'loaded':fetch?.status==='error'?'error':'pending',llmStatus:extraction?'extracted':llm?.status==='error'?'error':'pending',readyForImport:eligible&&!!raw&&!!extraction,fetchError:fetch?.status==='error'?fetch:null,llmError:llm?.status==='error'?llm:null,snapshot:latest?{...latest,markdown_raw:undefined}:null,extraction:extraction?{...extraction,extractedData:JSON.parse(extraction.extracted_data)}:null};
 if(includeMarkdown)return {...state,markdown:latest?await snapshotMarkdown(db,latest):null,history:{snapshots:(await db.prepare('SELECT id,content_hash,source_url,final_url,content_status,fetched_at,firecrawl_mode,firecrawl_credits FROM vacancy_raw_snapshots WHERE candidate_id=? ORDER BY created_at DESC,rowid DESC LIMIT 100').bind(c.id).all()).results||[],extractions:(await db.prepare('SELECT * FROM vacancy_extractions WHERE candidate_id=? ORDER BY processed_at DESC,rowid DESC LIMIT 100').bind(c.id).all()).results||[]}};
 return state;
}
