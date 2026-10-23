import {candidateDb,candidateHash,upsertVacancyCandidate} from '../repositories/vacancy-candidates.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
import {parserCompanyDb} from '../repositories/parser-company-scope.js';
import {readVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {apifySettings,apifyToken,apifyTokenHash} from '../repositories/apify-settings.js';
import {withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {schedulerSettings} from '../repositories/scheduler.js';
import {apifyError,apifyRequest,APIFY_RUN_CAP_USD} from '../integrations/apify.js';
import {apifyDatasetInfo,apifyDatasetItems} from './apify-runs.js';
import {apifyVacancy,saveApifySnapshot} from './apify-vacancies.js';
import {enqueueCandidateClassifications} from './scheduler-pipeline.js';
const UUID='[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}';
const operation=new RegExp('^listing:('+UUID+'):([0-9]{1,2})$');
export const APIFY_REPLAY_ROWS_PER_TICK=20;
async function scope(env,ledgerId){
 let db=await candidateDb(env.DB);const generation=boundGeneration(db),s=await schedulerSettings(db),a=await apifySettings(db);
 if(!s.enabled||!s.sourceIds.includes('hh')||!a.enabled)throw apifyError('apify_disabled',400);
 db=generationBoundDb(db,generation,{sql:"EXISTS(SELECT 1 FROM firecrawl_settings WHERE id=1 AND json_extract(scheduler_json,'$.revision')=? AND json_extract(scheduler_json,'$.enabled')=1 AND json_extract(scheduler_json,'$.ownerName')=?) AND EXISTS(SELECT 1 FROM apify_settings WHERE id=1 AND enabled=1 AND revision=?)",bindings:[s.revision,s.ownerName,a.revision],failure:()=>apifyError('apify_changed')});
 const ledger=await db.prepare("SELECT * FROM apify_runs WHERE id=? AND generation=? AND state IN ('succeeded','failed') AND charged_usd IS NOT NULL AND run_id<>'' AND dataset_id<>''").bind(ledgerId,generation).first(),m=ledger?.operation_key.match(operation);
 if(!ledger||!m||!/^[A-Za-z0-9]+$/.test(ledger.run_id)||!/^[A-Za-z0-9]+$/.test(ledger.dataset_id)||ledger.profile==='starter'&&!a.paid_allowed||await apifyTokenHash(env,a,ledger.profile)!==ledger.key_hash)throw apifyError('apify_changed');
 // Parent is server-owned, retained proof of company/source/user, not a URL
 // supplied by the caller. Active previews are left to their normal consumer.
 const parent=await db.prepare("SELECT * FROM vacancy_discovery_runs WHERE id=? AND generation=? AND user_name=? AND source_id='hh' AND json_extract(result_json,'$.paused') IS NULL AND coalesce(json_extract(result_json,'$.kind'),'')<>'apify-listing-replay'").bind(m[1],generation,s.ownerName).first();
 if(!parent)throw apifyError('apify_changed');db=await parserCompanyDb(db,parent.company_id,s.ownerName);
 const source=await readVacancyParsingSource(db,parent.company_id,'hh');
 if(!source.enabled||!source.supported||source.url!==parent.source_url||source.revision!==parent.source_revision)throw apifyError('apify_changed');
 db=generationBoundDb(db,generation,{sql:"EXISTS(SELECT 1 FROM vacancy_source_parsing WHERE company_id=? AND source_id='hh' AND enabled=1 AND revision=? AND source_url=?)",bindings:[source.companyId,source.revision,source.url],failure:()=>apifyError('apify_changed')});
 db=generationBoundDb(db,generation,{sql:'EXISTS(SELECT 1 FROM apify_runs WHERE id=? AND generation=? AND state=? AND run_id=? AND dataset_id=? AND key_hash=? AND charged_usd IS NOT NULL)',bindings:[ledger.id,generation,ledger.state,ledger.run_id,ledger.dataset_id,ledger.key_hash],failure:()=>apifyError('apify_changed')});
 return {db,source,ledger,owner:s.ownerName,settings:a,parentResult:JSON.parse(parent.result_json),parentId:parent.id,page:Number(m[2]),generation};
}
export async function inspectApifyListingReplays(env){
 const db=await candidateDb(env.DB),rows=(await db.prepare("SELECT id FROM apify_runs WHERE generation=? AND operation_key LIKE 'listing:%' AND state IN ('succeeded','failed') AND charged_usd IS NOT NULL ORDER BY created_at DESC,id LIMIT 20").bind(boundGeneration(db)).all()).results||[],candidates=[],skipped={};
 for(const row of rows){try{const x=await scope(env,row.id);candidates.push({ledgerId:row.id,companyId:x.source.companyId,page:x.page,profile:x.ledger.profile,sourceRunState:x.ledger.state,chargedUsd:x.ledger.charged_usd,parentStatus:['complete','partial','error'].includes(x.parentResult.status)?x.parentResult.status:null,parentFound:Number.isSafeInteger(x.parentResult.found)?x.parentResult.found:null,parentErrors:(x.parentResult.errors||[]).slice(0,5).map(e=>e.code).filter(c=>typeof c==='string'&&/^[a-z_]{1,48}$/.test(c))});}catch(e){if(['restored','scheduler_changed','job_changed'].includes(e.code))throw e;const code=e.code||'source_unavailable';skipped[code]=(skipped[code]||0)+1;}}
 return {candidates,skipped,actorStarts:0,externalRequests:0};
}
async function persist(x,state){
 const r=await x.db.prepare("INSERT INTO vacancy_discovery_runs(id,user_name,generation,company_id,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,?,?,'hh',?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET result_json=excluded.result_json,expires_at=excluded.expires_at WHERE vacancy_discovery_runs.generation=excluded.generation AND json_extract(vacancy_discovery_runs.result_json,'$.replay.ledgerId')=?").bind(state.runId,x.owner,x.generation,x.source.companyId,x.source.url,x.source.revision,x.settings.revision,JSON.stringify(state),state.createdAt,Date.now()+86400000,x.ledger.id).run();
 if(!r.meta?.changes)throw apifyError('apify_changed');
}
const sameMetadata=(a,b)=>a.itemCount===b.itemCount&&a.modifiedAt===b.modifiedAt;
export async function replayApifyListing(env,ledgerId,{signal}={}){
 if(signal?.aborted)throw signal.reason;
 env={...env,DB:await candidateDb(env.DB)};
 return withFirecrawlLease(env.DB,async(assertOwned,owned)=>{
  const x=await scope({...env,DB:owned},ledgerId),runId=await candidateHash('apify-listing-replay:'+x.generation+':'+ledgerId),prefix='replay:'+runId+':';
  const old=await x.db.prepare('SELECT result_json FROM vacancy_discovery_runs WHERE id=? AND generation=?').bind(runId,x.generation).first();
  let state=old?JSON.parse(old.result_json):{kind:'apify-listing-replay',runId,source:{companyId:x.source.companyId,sourceId:'hh',url:x.source.url},sourceRevision:x.source.revision,replay:{ledgerId,runId:x.ledger.run_id,datasetId:x.ledger.dataset_id},createdAt:Date.now(),offset:0,found:0,quarantinedCount:0,quarantine:[],complete:false,coverage:{datasetComplete:false,hhComplete:false},paused:{code:'replay_slice'}};
  if(state.replay?.ledgerId!==ledgerId||state.replay?.datasetId!==x.ledger.dataset_id||state.replay?.runId!==x.ledger.run_id||state.source.url!==x.source.url||state.sourceRevision!==x.source.revision)throw apifyError('apify_changed');
  const startOffset=state.offset,token=await apifyToken(env,x.settings,x.ledger.profile);
  const createdRows=async()=>(await x.db.prepare("SELECT id FROM vacancy_candidates WHERE creation_token>=? AND creation_token<? AND company_id=? AND source_id='hh' ORDER BY id LIMIT 100").bind(prefix,prefix+'g',x.source.companyId).all()).results||[];
  // Repair the save/enqueue boundary after a lost acknowledgement. Stable queue
  // keys never reset cancelled/failed jobs or call a model inside this operation.
  const enqueue=async()=>{const ids=(await createdRows()).map(r=>r.id);if(ids.length)await enqueueCandidateClassifications({...env,DB:x.db},ids);return ids;};
  if(!state.complete){
   if(x.ledger.state==='failed'){
    // Read-only terminal proof before consuming a failed run's fixed dataset.
    // Do not reconcile billing or turn the old preview/source into success.
    const run=(await apifyRequest(token,'/actor-runs/'+x.ledger.run_id,{signal,maxBytes:64000})).data;
    if(run?.id!==x.ledger.run_id||run.defaultDatasetId!==x.ledger.dataset_id||run.userId&&run.userId!==x.ledger.account_id||!['FAILED','TIMED-OUT','ABORTED'].includes(run?.status))throw apifyError('apify_changed');
    state.providerStatus=run.status;
   }
   await assertOwned();const metadata=await apifyDatasetInfo(token,x.ledger.dataset_id,{signal});
   if(metadata.itemCount>100||!metadata.modifiedAt||Date.now()-Date.parse(metadata.modifiedAt)<5000||state.metadata&&!sameMetadata(state.metadata,metadata))throw apifyError('apify_data',502);
   state.metadata=metadata;await persist(x,state);
   while(state.offset<metadata.itemCount&&state.offset-startOffset<APIFY_REPLAY_ROWS_PER_TICK){
    await assertOwned();await x.db.prepare('SELECT 1').first();
    const items=await apifyDatasetItems(token,x.ledger.dataset_id,state.offset,{signal});if(items.length!==Math.min(10,metadata.itemCount-state.offset))throw apifyError('apify_data',502);
    for(const item of items){
     if(signal?.aborted)throw signal.reason;
     let job;try{job=apifyVacancy(item,{sourceUrl:x.source.url});}catch(e){if(e.code!=='apify_data')throw e;state.quarantinedCount++;if(state.quarantine.length<20)state.quarantine.push({offset:state.offset,code:'invalid_vacancy'});state.offset++;continue;}
     let result;try{result=await upsertVacancyCandidate(x.db,{titleRaw:job.title,vacancyUrl:job.url,companyId:x.source.companyId,companyNameRaw:x.source.companyName,sourceId:'hh',sourceUrl:x.source.url,sourceNamespace:'company:'+x.source.companyId+':hh:https://hh.ru',externalJobId:job.externalJobId,acquisitionMethod:'site'},{onlyNew:true,intakeParser:'apify',creationToken:prefix+await candidateHash(job.url)});}catch(e){if(e.code!=='candidate_identity')throw e;state.quarantinedCount++;if(state.quarantine.length<20)state.quarantine.push({offset:state.offset,code:'identity_conflict'});state.offset++;continue;}
     if(result.candidate.companyId!==x.source.companyId){state.quarantinedCount++;if(state.quarantine.length<20)state.quarantine.push({offset:state.offset,code:'candidate_scope'});}
     else{state.found++;if(result.inserted)await saveApifySnapshot(x.db,result.id,job,{profile:x.ledger.profile,runId:x.ledger.run_id});}
     state.offset++;
    }
    await persist(x,state);
   }
   if(state.offset===metadata.itemCount){if(!sameMetadata(metadata,await apifyDatasetInfo(token,x.ledger.dataset_id,{signal})))throw apifyError('apify_data',502);state.complete=true;state.paused=null;state.coverage.datasetComplete=true;}
  }
  if(signal?.aborted)throw signal.reason;
  const createdCandidateIds=await enqueue();state.createdCandidates=createdCandidateIds.length;state.existingRows=Math.max(0,state.found-state.createdCandidates);state.status=state.complete&&x.ledger.state==='succeeded'&&!state.quarantinedCount&&x.ledger.charged_usd<APIFY_RUN_CAP_USD-1e-9?'complete':'partial';await persist(x,state);
  return {ledgerId,companyId:x.source.companyId,sourceRunState:x.ledger.state,providerStatus:state.providerStatus||null,status:state.status,rawRows:state.offset,processed:state.offset-startOffset,found:state.found,createdCandidates:state.createdCandidates,existingRows:state.existingRows,createdCandidateIds,quarantinedCount:state.quarantinedCount,quarantine:state.quarantine,complete:state.complete,coverage:state.coverage,providerCapReached:x.ledger.charged_usd>=APIFY_RUN_CAP_USD-1e-9,actorStarts:0,modelCalls:0};
 },110000);
}
