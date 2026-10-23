import {APIFY_PLANS,APIFY_RUN_CAP_USD,APIFY_READ_RESERVE_USD} from '../integrations/apify.js';
// Account usage and confirmed local charges measure overlapping spending. Use
// the larger total, then add unresolved run caps and read reserves. This remains
// an estimate: the provider does not expose a per-run billing watermark.
export const APIFY_CONFIRMED_SQL='coalesce(sum(charged_usd),0)';
export const APIFY_UNRESOLVED_SQL=`coalesce(sum(CASE WHEN charged_usd IS NULL THEN reserved_usd ELSE 0 END),0)+count(*)*${APIFY_READ_RESERVE_USD}`;
export const apifyAccountUsageId=account=>JSON.stringify([account.accountId,account.periodStart,account.periodEnd]);
export const apifyBudgetLimit=(profile,account)=>Math.min(APIFY_PLANS[profile].creditsUsd,account.planCreditsUsd,account.providerLimitUsd);
export async function apifyBudget(db,profile,account){
 const row=await db.prepare(`SELECT (SELECT provider_used_usd FROM apify_account_usage WHERE id=?) provider_used,${APIFY_CONFIRMED_SQL} confirmed,coalesce(sum(CASE WHEN charged_usd IS NULL THEN reserved_usd ELSE 0 END),0) unknown,count(*) runs,sum(CASE WHEN charged_usd IS NULL THEN 1 ELSE 0 END) unknown_count FROM apify_runs WHERE account_id=? AND period_start=? AND period_end=?`).bind(apifyAccountUsageId(account),account.accountId,account.periodStart,account.periodEnd).first();
 const limitUsd=apifyBudgetLimit(profile,account),providerUsedUsd=Math.max(account.providerUsedUsd,row.provider_used||0),readReserveUsd=row.runs*APIFY_READ_RESERVE_USD,localUpperUsd=row.confirmed+row.unknown+readReserveUsd,estimatedUsedUsd=Math.max(providerUsedUsd,row.confirmed)+row.unknown+readReserveUsd;
 return {...account,providerUsedUsd,limitUsd,localUpperUsd,estimatedUsedUsd,ownConfirmedUsd:row.confirmed,unknownReserveUsd:row.unknown,unknownRuns:row.unknown_count||0,readReserveUsd,coverageStatus:'estimated_overlap',remainingUsd:Math.max(0,limitUsd-estimatedUsedUsd),minimumReserveUsd:APIFY_RUN_CAP_USD+APIFY_READ_RESERVE_USD};
}
