import {candidateDb} from '../repositories/vacancy-candidates.js';
import {APIFY_ACTOR,APIFY_RUN_CAP_USD,APIFY_READ_RESERVE_USD,apifyRequest,apifyError} from '../integrations/apify.js';
import {apifySettings,apifyToken,apifyTokenHash} from '../repositories/apify-settings.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {refreshApifyAccount} from './apify-account.js';
import {APIFY_CONFIRMED_SQL,APIFY_UNRESOLVED_SQL,apifyBudgetLimit,apifyAccountUsageId} from './apify-budget.js';
const idPattern=/^[A-Za-z0-9]+$/;
function checkedRun(value){if(!value||!idPattern.test(value.id)||!idPattern.test(value.defaultDatasetId)||!['READY','RUNNING','SUCCEEDED','FAILED','TIMED-OUT','ABORTING','ABORTED','TIMING-OUT'].includes(value.status))throw apifyError('apify_data',502);return value;}
export async function recordApifyRun(db,row,run){
 if(run.id!==row.run_id&&row.run_id||row.dataset_id&&run.defaultDatasetId!==row.dataset_id||run.userId&&run.userId!==row.account_id)throw apifyError('apify_data',502);
 const state=run.status==='SUCCEEDED'?'succeeded':['FAILED','TIMED-OUT','ABORTED'].includes(run.status)?'failed':'running',recent=Number.isFinite(Date.parse(run.finishedAt))&&Date.now()-Date.parse(run.finishedAt)<10000,cost=typeof run.usageTotalUsd==='number'&&Number.isFinite(run.usageTotalUsd)&&run.usageTotalUsd>=0&&state!=='running'&&!recent?run.usageTotalUsd:null;
 // Never erase a known charge when an eventually consistent read omits it.
 const charged=cost===null?row.charged_usd:Math.max(cost,row.charged_usd??0),now=Date.now();
 const saved=await db.prepare("UPDATE apify_runs SET state=CASE WHEN state IN ('succeeded','failed') AND ?='running' THEN state ELSE ? END,run_id=?,dataset_id=?,charged_usd=CASE WHEN ? IS NULL THEN charged_usd ELSE max(coalesce(charged_usd,0),?) END,error_code=?,updated_at=? WHERE id=? AND run_id=? AND dataset_id=?").bind(state,state,run.id,run.defaultDatasetId,charged,charged,state==='failed'?'apify_failed':'',now,row.id,row.run_id,row.dataset_id).run();
 if(!saved.meta?.changes)throw apifyError('apify_changed');
 return {...await db.prepare('SELECT * FROM apify_runs WHERE id=?').bind(row.id).first(),build_id:typeof run.buildId==='string'?run.buildId:null};
}
// Bounded existing-run GETs only; never launches, resurrects or clears uncertain IDs.
export async function reconcileApifyRuns(env,{signal,limit=4,profile,deadline=Infinity}={}){
 const db=env.DB,s=await apifySettings(db),rows=(await db.prepare(`SELECT * FROM apify_runs WHERE run_id<>'' AND (? IS NULL OR profile=?) AND (state IN ('running','starting','uncertain') OR charged_usd IS NULL AND updated_at<? OR updated_at<?) ORDER BY CASE WHEN state IN ('running','starting','uncertain') THEN 0 WHEN charged_usd IS NULL THEN 1 ELSE 2 END,updated_at,id LIMIT ?`).bind(profile??null,profile??null,Date.now()-10000,Date.now()-300000,Math.min(4,Math.max(1,limit))).all()).results||[],results=[];
 for(const row of rows){
  if(Date.now()>=deadline)break;
  try{
   if(row.profile==='starter'&&!s.paid_allowed){results.push({state:row.state,code:'disabled'});continue;}
   if(!idPattern.test(row.run_id)||await apifyTokenHash(env,s,row.profile)!==row.key_hash)throw apifyError('apify_changed');
   const run=checkedRun((await apifyRequest(await apifyToken(env,s,row.profile),'/actor-runs/'+row.run_id,{signal})).data);
   const recorded=await recordApifyRun(db,row,run);
   results.push({state:recorded.state,costKnown:recorded.charged_usd!==null});
  }catch(e){if(signal?.aborted||['restored','job_changed'].includes(e.code))throw e;results.push({state:row.state,code:e.code||'apify_network'});}
 }
 return {checked:results.length,results};
}
export async function executeApifyRun(env,{operationKey,input,signal,includeRunMetadata=false,allowFailedDataset=false}){
 env={...env,DB:await candidateDb(env.DB)};const db=env.DB,generation=boundGeneration(db);let row=await db.prepare('SELECT * FROM apify_runs WHERE generation=? AND operation_key=?').bind(generation,operationKey).first();
 const s=await apifySettings(db);if(!s.enabled)throw apifyError('apify_disabled',400);
 if(!row){
  env.SCHEDULER_APIFY_ADMISSION?.assertCanStart();
  let selected;
  for(const profile of ['free','starter']){
   if(profile==='starter'&&!s.paid_allowed)break;
   if(!s[profile+'_key_encrypted'])continue;
   await reconcileApifyRuns(env,{profile,signal,limit:2});
   const {account,budget}=await refreshApifyAccount(env,profile,{signal});
   // Free must really be a $0 account. Paid accounts require explicit permission.
   if(profile==='free'&&account.planPriceUsd!==0||profile==='starter'&&(account.planPriceUsd<19||account.planCreditsUsd<19))continue;
   if(budget.remainingUsd+1e-9<APIFY_RUN_CAP_USD+APIFY_READ_RESERVE_USD)continue;
   selected={profile,account};break;
  }
  if(!selected)throw apifyError(s.free_key_encrypted||s.starter_key_encrypted?'apify_budget':'apify_disabled',402);
  const {profile,account}=selected,id=crypto.randomUUID(),now=Date.now(),hash=await apifyTokenHash(env,s,profile),cap=APIFY_RUN_CAP_USD+APIFY_READ_RESERVE_USD;
  // Reserve this tick's start allowance before creating a durable operation.
  // A quota denial must not leave a failed/uncertain empty Actor identity.
  env.SCHEDULER_APIFY_ADMISSION?.takeStart();
  // Claim, budget, snapshot and global single flight in ONE transaction/write.
  const r=await db.prepare(`INSERT OR IGNORE INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at)
   SELECT ?,?,?,?,?,?,?,?,?,'starting',?,? WHERE EXISTS(SELECT 1 FROM apify_settings WHERE id=1 AND enabled=1 AND revision=? AND (?='free' OR paid_allowed=1))
   AND NOT EXISTS(SELECT 1 FROM apify_runs WHERE state IN ('starting','running','uncertain') AND created_at>?)
   AND EXISTS(SELECT 1 FROM apify_settings WHERE id=1 AND json_extract(${profile}_account_json,'$.accountId')=? AND json_extract(${profile}_account_json,'$.periodStart')=? AND json_extract(${profile}_account_json,'$.periodEnd')=? AND max(?,json_extract(${profile}_account_json,'$.providerUsedUsd'),coalesce((SELECT provider_used_usd FROM apify_account_usage WHERE id=?),0),(SELECT ${APIFY_CONFIRMED_SQL} FROM apify_runs WHERE account_id=? AND period_start=? AND period_end=?))+(SELECT ${APIFY_UNRESOLVED_SQL} FROM apify_runs WHERE account_id=? AND period_start=? AND period_end=?)+?<=?)`).bind(id,generation,operationKey,profile,account.accountId,hash,account.periodStart,account.periodEnd,APIFY_RUN_CAP_USD,now,now,s.revision,profile,now-300000,account.accountId,account.periodStart,account.periodEnd,account.providerUsedUsd,apifyAccountUsageId(account),account.accountId,account.periodStart,account.periodEnd,account.accountId,account.periodStart,account.periodEnd,cap,apifyBudgetLimit(profile,account)).run();
  if(!r.meta?.changes){row=await db.prepare('SELECT * FROM apify_runs WHERE generation=? AND operation_key=?').bind(generation,operationKey).first();if(!row)throw apifyError('apify_busy');}
  else{
   row=await db.prepare('SELECT * FROM apify_runs WHERE id=?').bind(id).first();
   const launch=async()=>{
    const fresh=await apifySettings(db);if(fresh.revision!==s.revision||!fresh.enabled||profile==='starter'&&!fresh.paid_allowed)throw apifyError('apify_changed');
    const token=await apifyToken(env,s,profile);signal?.throwIfAborted();
    const run=checkedRun((await apifyRequest(token,'/actors/'+APIFY_ACTOR+'/runs?timeout=180&maxTotalChargeUsd='+APIFY_RUN_CAP_USD+'&restartOnError=false&waitForFinish=0',{method:'POST',body:input,signal})).data);
    row=await recordApifyRun(db,row,run);return {usage:{},run};
   };
   try{if(env.SCHEDULER_BILLING)await env.SCHEDULER_BILLING.run({provider:'apify',model:APIFY_ACTOR,free:profile==='free',capUsd:cap},launch);else await launch();}
   catch(e){
    // No start retry, including lost ACK. Preserve reservation across restore.
    if(['scheduler_budget','apify_changed','apify_cycle_limit'].includes(e.code))await db.prepare("UPDATE apify_runs SET state='failed',charged_usd=0,error_code=?,updated_at=? WHERE id=? AND state='starting'").bind(e.code,Date.now(),id).run();
    else try{await db.prepare("UPDATE apify_runs SET state='uncertain',error_code='apify_uncertain',updated_at=? WHERE id=? AND state='starting'").bind(Date.now(),id).run();}catch{}
    throw e;
   }
  }
 }else if(row.profile==='starter'&&!s.paid_allowed)throw apifyError('apify_disabled',400);
 if(['starting','uncertain'].includes(row.state))throw apifyError('apify_uncertain');
 const token=await apifyToken(env,s,row.profile);if(await apifyTokenHash(env,s,row.profile)!==row.key_hash)throw apifyError('apify_changed');
 if(row.state==='running'||row.charged_usd===null&&Date.now()-row.updated_at>=10000||includeRunMetadata&&row.state==='succeeded'&&!row.build_id)row=await recordApifyRun(db,row,checkedRun((await apifyRequest(token,'/actor-runs/'+row.run_id,{signal})).data));
 const failed=row.state==='failed';
 // Listing collection can salvage a fixed terminal dataset, never restart it.
 // Detail callers retain their strict failure behavior.
 if(failed&&(!allowFailedDataset||!idPattern.test(row.run_id)||!idPattern.test(row.dataset_id)))throw apifyError('apify_failed',422);
 return {row,token,pending:!failed&&row.state!=='succeeded',...(failed?{failed:true}:{})};
}
export async function apifyDatasetItems(token,datasetId,offset,{signal,clean=false}={}){
 if(!idPattern.test(datasetId)||!Number.isSafeInteger(offset)||offset<0)throw apifyError('apify_data',400);
 const items=await apifyRequest(token,'/datasets/'+datasetId+'/items?format=json&clean='+String(clean)+'&skipHidden=true&skipEmpty='+String(clean)+'&offset='+offset+'&limit=10',{signal,maxBytes:1800000});
 if(!Array.isArray(items)||items.length>10)throw apifyError('apify_data',502);return items;
}
export async function apifyDatasetInfo(token,datasetId,{signal}={}){
 if(!idPattern.test(datasetId))throw apifyError('apify_data',400);
 const d=(await apifyRequest(token,'/datasets/'+datasetId,{signal,maxBytes:64000})).data;
 if(d?.id!==datasetId||!Number.isSafeInteger(d.itemCount)||d.itemCount<0)throw apifyError('apify_data',502);
 return {itemCount:d.itemCount,cleanItemCount:Number.isSafeInteger(d.cleanItemCount)&&d.cleanItemCount>=0?d.cleanItemCount:null,...(typeof d.modifiedAt==='string'&&Number.isFinite(Date.parse(d.modifiedAt))?{modifiedAt:d.modifiedAt}:{})};
}
