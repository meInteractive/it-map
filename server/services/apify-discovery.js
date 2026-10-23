import {parserOwner,parserOwnerColumns} from '../repositories/parser-owner.js';
import {operationErrorCode} from './operation-error.js';
import {hhInput,apifyError,APIFY_RUN_CAP_USD} from '../integrations/apify.js';
import {apifySettings} from '../repositories/apify-settings.js';
import {parserCompanyDb} from '../repositories/parser-company-scope.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {candidateHash} from '../repositories/vacancy-candidates.js';
import {executeApifyRun,apifyDatasetItems,apifyDatasetInfo} from './apify-runs.js';
import {apifyVacancy} from './apify-vacancies.js';
import {knownCandidates} from './vacancy-discovery.js';
import {readHHTitleFilter} from './hh-title-filter.js';
const TTL=3600000;
async function persist(db,result,userName,revision){
 await db.batch([
  db.prepare(`SELECT CASE WHEN EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE ${parserOwner(result.source).column}=? AND source_id=? AND source_url=? AND revision=? AND enabled=1) THEN 1 ELSE abs(-9223372036854775808) END`).bind(parserOwner(result.source).id,result.source.sourceId,result.source.url,result.sourceRevision),
  db.prepare('INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,agency_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET result_json=excluded.result_json,settings_revision=excluded.settings_revision,expires_at=excluded.expires_at WHERE vacancy_discovery_runs.user_name=excluded.user_name AND vacancy_discovery_runs.generation=excluded.generation').bind(result.runId,userName,boundGeneration(db),result.source.companyId,result.source.agencyId??null,result.source.sourceId,result.source.url,result.sourceRevision,revision,JSON.stringify(result),Date.now(),Date.now()+TTL)
 ]);return result;
}
export async function fetchApifyListing(env,companyId,sourceId,{userName,signal,resume,discoveryId}={}){
 if(sourceId!=='hh')throw apifyError('apify_scope',400);
 return withFirecrawlLease(env.PARSER_RESOURCE_DB||env.DB,async(assertOwned,owned)=>{
  const db=await parserCompanyDb(owned,companyId,userName);env={...env,DB:db};const source=await readVacancyParsingSource(db,companyId,sourceId),settings=await apifySettings(db);
  if(!source.enabled||!settings.enabled)throw apifyError('apify_disabled',400);hhInput(source.url);
  if(resume&&(resume.source.url!==source.url||resume.sourceRevision!==source.revision))throw apifyError('apify_changed');
  const runId=resume?.runId||discoveryId||crypto.randomUUID();
  if(!resume){const old=await db.prepare('SELECT result_json FROM vacancy_discovery_runs WHERE id=? AND user_name=? AND generation=?').bind(runId,userName,boundGeneration(db)).first();if(old)resume=JSON.parse(old.result_json);}
  if(resume&&!resume.paused)return resume;
  if(resume?.jobs?.length&&(!resume.savedAt||resume.saveErrors?.length))throw Object.assign(apifyError('apify_changed'),{message:'Сначала сохраните найденные вакансии текущей порции.'});
  // Freeze ordering before the paid operation. Legacy cursors keep their old
  // order on every page; switching mid-scan can skip or duplicate vacancies.
  const cursor={...(resume?.paused?.apify||{page:0,offset:0,pageCount:0,seenPages:[],orderBy:'publication_time',titleFilter:await readHHTitleFilter(db)})};
  // Old clean offsets are ambiguous. Replay this dataset from raw row zero,
  // keeping the same paid operation identity; candidate IDs deduplicate writes.
  if(cursor.rowMode!=='raw-v1'&&cursor.offset){cursor.offset=0;cursor.pageCount=0;cursor.seenPages=cursor.seenPages.slice(0,-1);}
  cursor.rowMode='raw-v1';
  const input=hhInput(source.url,{page:cursor.page,orderBy:cursor.orderBy,titleQuery:cursor.titleFilter?.query});
  const paused={code:'apify_running',pageUrl:input.startUrl,remainingPages:1,visited:[],apify:cursor};
  const base={runId,provider:'apify',sourceRevision:source.revision,paused,status:'partial',source:{...parserOwnerColumns(source),companyName:source.companyName,sourceId,sourceName:source.sourceName,url:source.url},mode:'apify',adapter:'hh',proxy:'apify-configured',maxPages:cursor.page+1,requests:resume?.requests||0,loadedPages:resume?.loadedPages||0,found:resume?.found||0,newCount:0,existingCount:0,savedNew:0,savedExisting:0,saveErrors:[],durationMs:resume?.durationMs||0,creditsReported:null,creditsEstimate:0,creditEstimateNote:'Apify учитывается отдельно в USD бюджета Free/Starter.',pages:[],errors:[],warnings:[],jobs:[],expiresAt:new Date(Date.now()+TTL).toISOString()};
  base.titleFilter=cursor.titleFilter??{version:0,query:null,status:'legacy_unfiltered'};
  base.rawRows=resume?.rawRows||0;base.quarantinedCount=resume?.quarantinedCount||0;base.emptyCount=resume?.emptyCount||0;base.quarantine=[...(resume?.quarantine||[])];
  base.coverage={datasetComplete:false,hhComplete:false};
  // Persist the operation identity BEFORE the paid start can be sent.
  await persist(db,base,userName,settings.revision);const started=Date.now();
  try{
   const execution=await executeApifyRun(env,{operationKey:'listing:'+runId+':'+cursor.page,input,signal,allowFailedDataset:true});await assertOwned();base.apifyProfile=execution.row.profile;base.apifyRunId=execution.row.run_id;base.apifyBuildId=execution.row.build_id||resume?.apifyBuildId;
   base.requests=Math.max(resume?.requests||0,cursor.page+1);
   if(execution.pending)return persist(db,{...base,durationMs:base.durationMs+Date.now()-started},userName,settings.revision);
   const metadata=await apifyDatasetInfo(execution.token,execution.row.dataset_id,{signal});
   const items=await apifyDatasetItems(execution.token,execution.row.dataset_id,cursor.offset,{signal}),jobs=[];
   if(cursor.offset===0&&items.length){const fingerprint=await candidateHash(JSON.stringify(items.map(j=>[j?.vacancy_id,j?.url])));if(cursor.seenPages.includes(fingerprint))cursor.repeatedPrefix=true;else cursor.seenPages=[...cursor.seenPages,fingerprint];}
   for(const [index,item] of items.entries()){
    try{jobs.push(apifyVacancy(item,{sourceUrl:source.url}));}
    catch(e){if(e.code!=='apify_data')throw e;const empty=!item||typeof item!=='object'||!Object.keys(item).length;base.quarantinedCount++;if(empty)base.emptyCount++;if(base.quarantine.length<20)base.quarantine.push({page:cursor.page,offset:cursor.offset+index,code:empty?'empty_record':'invalid_vacancy'});}
   }
   base.jobs=await knownCandidates(db,jobs,source);base.found+=jobs.length;base.newCount=base.jobs.filter(j=>!j.existingId).length;base.existingCount=jobs.length-base.newCount;cursor.pageCount+=items.length;
   base.rawRows+=items.length;cursor.offset+=items.length;
   const stableAt=metadata.modifiedAt?Date.parse(metadata.modifiedAt):execution.row.updated_at;
   const datasetEnd=items.length<10&&cursor.offset===metadata.itemCount&&Date.now()-stableAt>=5000;
   if(items.length<10&&cursor.offset!==metadata.itemCount){cursor.metadataMismatchAt??=Date.now();if(Date.now()-cursor.metadataMismatchAt>=10000)throw Object.assign(apifyError('apify_data',422),{message:'Число исходных строк не согласуется с metadata dataset. Прогресс сохранён; полнота не подтверждена.'});}
   else delete cursor.metadataMismatchAt;
   if(!datasetEnd){base.paused={...paused,code:'slice',apify:cursor};}
   else if(execution.row.charged_usd===null){base.paused={...paused,code:'apify_running',apify:cursor};}
   else if(execution.failed){base.loadedPages++;base.paused=null;base.coverage.datasetComplete=true;base.errors=[{code:'apify_partial_run',message:'Actor завершился с ошибкой или тайм-аутом. Доступный dataset прочитан, корректные вакансии сохранены; полнота HH не подтверждена.',providerStatus:null,targetStatus:null}];}
   else if(execution.row.charged_usd>=APIFY_RUN_CAP_USD-1e-9){base.loadedPages++;base.paused=null;base.errors=[{code:'apify_run_limit',message:'Actor достиг денежного лимита. Найденные вакансии сохранены; полнота страницы HH не подтверждена.',providerStatus:null,targetStatus:null}];}
   else if(cursor.repeatedPrefix){base.loadedPages++;base.paused=null;base.coverage.datasetComplete=true;base.errors=[{code:'apify_pagination_uncertain',message:'Начало страницы HH повторилось. Оплаченный dataset прочитан, корректные вакансии сохранены. Следующая страница не запускалась; полнота HH не подтверждена.',providerStatus:null,targetStatus:null}];}
   else if(cursor.pageCount>=100&&cursor.page<99){base.loadedPages++;cursor.page++;cursor.offset=0;cursor.pageCount=0;delete cursor.repeatedPrefix;base.paused={...paused,code:'slice',apify:cursor};}
   else{
    base.loadedPages++;base.paused=null;base.coverage.datasetComplete=true;
    if(base.quarantinedCount||cursor.pageCount>=100){base.errors=[{code:base.quarantinedCount?'apify_partial_data':'apify_page_limit',message:'Корректные вакансии сохранены. Есть отклонённые записи или достигнут лимит страниц; сбор частичный.',providerStatus:null,targetStatus:null}];}
    else{base.status='complete';base.coverage.hhComplete=true;}
   }
   if(base.quarantinedCount)base.warnings.push('Отклонено записей: '+base.quarantinedCount+'. Диагностика хранит до20 позиций без исходных текстов.');
   base.durationMs+=Date.now()-started;return persist(db,base,userName,settings.revision);
  }catch(e){
   if(['restored','changed','source_scope','favorites_only','scheduler_changed','job_changed'].includes(e.code))throw e;
   const code=operationErrorCode(e,'apify_network');
   const retryable=['apify_budget','apify_busy','apify_network','apify_timeout','apify_uncertain','apify_disabled','apify_changed','apify_cycle_limit'].includes(code)||code==='timeout'||signal?.aborted;
   base.errors=[{code,message:e.status?e.message:'Ожидание Apify прервано. Прогресс сохранён.',providerStatus:null,targetStatus:null}];base.paused=retryable?{...paused,code}:null;base.status='error';base.durationMs+=Date.now()-started;
   if(!retryable)await db.prepare(`UPDATE vacancy_source_parsing SET last_error=? WHERE ${parserOwner(source).column}=? AND source_id=? AND revision=?`).bind(base.errors[0].message,parserOwner(source).id,sourceId,source.revision).run();
   return persist(db,base,userName,settings.revision);
  }
 },120000);
}
