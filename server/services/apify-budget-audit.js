import {apifySettings,apifyToken} from '../repositories/apify-settings.js';
import {apifyRequest,apifyError} from '../integrations/apify.js';
import {refreshApifyAccount} from './apify-account.js';
const finite=n=>typeof n==='number'&&Number.isFinite(n)&&n>=0;
// Aggregated service amounts do not identify billed run IDs or provide a cutoff.
// Keep only numeric billing metadata, never arbitrary user/run payloads.
export function monthlyApifySummary(data,account){
 if(!data||Date.parse(data.usageCycle?.startAt)!==Date.parse(account.periodStart)||Date.parse(data.usageCycle?.endAt)!==Date.parse(account.periodEnd)||!finite(data.totalUsageCreditsUsdAfterVolumeDiscount)||!data.monthlyServiceUsage||typeof data.monthlyServiceUsage!=='object'||Array.isArray(data.monthlyServiceUsage))throw apifyError('apify_data',502);
 const entries=Object.entries(data.monthlyServiceUsage),services=[];
 for(const [service,value] of entries.slice(0,128)){
  if(!/^[A-Za-z0-9_:~.-]{1,160}$/.test(service)||/apify_api_/i.test(service)||!value||!finite(value.quantity)||!finite(value.baseAmountUsd))continue;
  services.push({service,quantity:value.quantity,baseAmountUsd:value.baseAmountUsd,...(finite(value.amountAfterVolumeDiscountUsd)?{amountAfterVolumeDiscountUsd:value.amountAfterVolumeDiscountUsd}:{})});
 }
 return {periodStart:account.periodStart,periodEnd:account.periodEnd,totalAfterDiscountUsd:data.totalUsageCreditsUsdAfterVolumeDiscount,...(finite(data.totalUsageCreditsUsdBeforeVolumeDiscount)?{totalBeforeDiscountUsd:data.totalUsageCreditsUsdBeforeVolumeDiscount}:{}),services,servicesTruncated:services.length!==entries.length,checkedAt:Date.now(),runCoverageAvailable:false};
}
export async function auditApifyBudget(env,{signal}={}){
 const initial=await apifySettings(env.DB),profiles={};
 for(const profile of ['free','starter']){
  if(!initial[profile+'_key_encrypted']||profile==='starter'&&!initial.paid_allowed)continue;
  try{
   const assertSettings=async()=>{const s=await apifySettings(env.DB);if(s.revision!==initial.revision||s[profile+'_key_encrypted']!==initial[profile+'_key_encrypted']||profile==='starter'&&!s.paid_allowed)throw apifyError('apify_changed');};
   await assertSettings();const {budget,account}=await refreshApifyAccount(env,profile,{signal});await assertSettings();
   const data=(await apifyRequest(await apifyToken(env,initial,profile),'/users/me/usage/monthly',{signal})).data;
   await assertSettings();profiles[profile]={budget,monthly:monthlyApifySummary(data,account)};
  }catch(e){if(signal?.aborted||['restored','job_changed'].includes(e.code))throw e;profiles[profile]={code:e.code||'apify_network'};}
 }
 return {profiles,errors:Object.values(profiles).filter(p=>p.code).length,actorStarts:0};
}
