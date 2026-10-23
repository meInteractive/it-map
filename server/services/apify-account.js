import {apifySettings,apifyToken,publicApifySettings} from '../repositories/apify-settings.js';
import {apifyRequest,apifyProfile,apifyError} from '../integrations/apify.js';
const finite=value=>typeof value==='number'&&Number.isFinite(value)&&value>=0;
export {apifyBudget} from './apify-budget.js';
import {apifyBudget,apifyAccountUsageId} from './apify-budget.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
export async function refreshApifyAccount(env,profile,{signal}={}){
 apifyProfile(profile);env={...env,DB:await candidateDb(env.DB)};const s=await apifySettings(env.DB),token=await apifyToken(env,s,profile);
 try{
  // /users/me also contains unrelated secrets. Retain only explicit billing fields.
  const user=(await apifyRequest(token,'/users/me',{signal})).data;
  const usage=(await apifyRequest(token,'/users/me/limits',{signal})).data;
  const start=usage?.monthlyUsageCycle?.startAt,end=usage?.monthlyUsageCycle?.endAt,now=Date.now();
  if(typeof user?.id!=='string'||!/^[A-Za-z0-9]+$/.test(user.id)||!finite(user.plan?.monthlyBasePriceUsd)||!finite(user.plan?.monthlyUsageCreditsUsd)||!finite(usage?.current?.monthlyUsageUsd)||!finite(usage?.limits?.maxMonthlyUsageUsd)||!start||!end||!(Date.parse(start)<=now&&Date.parse(end)>now)||user.plan.isEnabled===false)throw apifyError('apify_data',502);
  const account={accountId:user.id,planName:String(user.plan.id||'').slice(0,80),planPriceUsd:user.plan.monthlyBasePriceUsd,planCreditsUsd:user.plan.monthlyUsageCreditsUsd,providerUsedUsd:usage.current.monthlyUsageUsd,providerLimitUsd:usage.limits.maxMonthlyUsageUsd,periodStart:start,periodEnd:end,checkedAt:now};
  // SQL high-water mark survives two refreshes completing out of order. Account
  // and period identity must match before carrying any usage forward.
  const field=profile+'_account_json',stored=`CASE WHEN json_valid(${field}) THEN ${field} ELSE '{}' END`;
  const same=`CASE WHEN json_extract(${stored},'$.accountId')=? AND json_extract(${stored},'$.periodStart')=? AND json_extract(${stored},'$.periodEnd')=? THEN coalesce(json_extract(${stored},'$.providerUsedUsd'),0) ELSE 0 END`,id=apifyAccountUsageId(account);
  const results=await env.DB.batch([
   env.DB.prepare(`INSERT INTO apify_account_usage(id,account_id,period_start,period_end,provider_used_usd,updated_at) SELECT ?,?,?,?,max(?,${same}),? FROM apify_settings WHERE id=1 AND revision=? ON CONFLICT(id) DO UPDATE SET provider_used_usd=max(provider_used_usd,excluded.provider_used_usd),updated_at=max(updated_at,excluded.updated_at)`).bind(id,account.accountId,start,end,account.providerUsedUsd,account.accountId,start,end,now,s.revision),
   env.DB.prepare(`UPDATE apify_settings SET ${field}=json_set(?, '$.providerUsedUsd',(SELECT provider_used_usd FROM apify_account_usage WHERE id=?)),${profile}_error='' WHERE id=1 AND revision=? RETURNING ${field} account`).bind(JSON.stringify(account),id,s.revision)
  ]);
  const saved=results[1].results?.[0];if(!saved)throw apifyError('apify_changed');account.providerUsedUsd=JSON.parse(saved.account).providerUsedUsd;
  return {account,budget:await apifyBudget(env.DB,profile,account),state:publicApifySettings(await apifySettings(env.DB))};
 }catch(e){if(e.code!=='restored'&&e.code!=='apify_changed')await env.DB.prepare(`UPDATE apify_settings SET ${profile}_error=? WHERE id=1 AND revision=?`).bind(e.status?e.message:'Статистика Apify недоступна.',s.revision).run();throw e;}
}
