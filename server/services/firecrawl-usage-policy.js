import {firecrawlSettings,firecrawlKey,profileColumns} from '../repositories/firecrawl-settings.js';
import {candidateDb,candidateHash} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {FIRECRAWL_PLANS,FIRECRAWL_MAX_SCRAPE_CREDITS,FIRECRAWL_STATS_MAX_AGE_MS,firecrawlMode} from '../integrations/firecrawl-plans.js';
import {parseCreditUsage} from '../integrations/firecrawl-credit-usage.js';
import {firecrawlScrape as rawScrape,testFirecrawlConnection as rawUsage,safeListingUrl,fcError,FirecrawlError} from '../integrations/firecrawl-transport.js';
const sessions=new WeakMap();
export async function createFirecrawlUsageSession(env,{signal,expectedRevision}={}){
 env={...env,DB:await candidateDb(env.DB)};const s=await firecrawlSettings(env.DB);if(expectedRevision!==undefined&&s.revision!==expectedRevision)throw fcError('profile_changed',409);
 const key=await firecrawlKey(env,s),keyHash=await candidateHash(key),session=Object.freeze({mode:s.mode,revision:s.revision});sessions.set(session,{env,s,key,keyHash,signal});return session;
}
// Account reads are the zero-credit operation of the same policy. No billing,
// subscription, top-up, Agent, JSON, crawl or arbitrary endpoint is exposed.
export async function readFirecrawlCreditUsage(env,{mode,signal}={}){
 env={...env,DB:await candidateDb(env.DB)};const s=await firecrawlSettings(env.DB),selected=firecrawlMode(mode??s.mode),key=await firecrawlKey(env,s,selected);
 return rawUsage(key,{signal,timeoutMs:s.timeout_ms,retry:false});
}
export async function recordFirecrawlAccount(db,keyHash,account){
 await db.prepare(`INSERT INTO firecrawl_usage_periods(key_hash,period_start,period_end,provider_used_high,remaining_credits,plan_credits,checked_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(key_hash,period_start,period_end) DO UPDATE SET provider_used_high=max(firecrawl_usage_periods.provider_used_high,excluded.provider_used_high),remaining_credits=excluded.remaining_credits,plan_credits=excluded.plan_credits,checked_at=excluded.checked_at`).bind(keyHash,account.periodStart,account.periodEnd,account.providerUsedEstimate,account.remainingCredits,account.planCredits,account.checkedAt).run();
}
export async function firecrawlBudgetSnapshot(db,mode,keyHash,account){
 const now=Date.now(),plan=FIRECRAWL_PLANS[mode];
 const period=await db.prepare('SELECT * FROM firecrawl_usage_periods WHERE key_hash=? AND period_start=? AND period_end=?').bind(keyHash,account.periodStart,account.periodEnd).first();
 const totals=await db.prepare(`SELECT coalesce(sum(coalesce(charged_credits,reserved_credits)),0) AS localUpper,coalesce(sum(CASE WHEN outcome='reserved' AND active_until>? THEN reserved_credits ELSE 0 END),0) AS reserved,coalesce(sum(CASE WHEN charged_credits IS NOT NULL THEN charged_credits ELSE 0 END),0) AS reported,coalesce(sum(CASE WHEN charged_credits IS NULL THEN reserved_credits ELSE 0 END),0) AS estimated FROM firecrawl_usage_events WHERE period_start=? AND period_end=? AND (profile=? OR key_hash=?)`).bind(now,account.periodStart,account.periodEnd,mode,keyHash).first();
 // The API does not certify which local requests a balance snapshot includes.
 // Add all local exposure to provider high-water instead of assuming immediate
 // billing visibility. This may count known charges twice, but is fail-closed.
 const providerHigh=period?.provider_used_high??account.providerUsedEstimate,effectiveUsed=providerHigh+totals.localUpper;
 return {limit:plan.credits,providerRemaining:account.remainingCredits,providerPlanCredits:account.planCredits,providerUsedEstimate:account.hasBonusBalance?null:account.providerUsedEstimate,providerUsedHigh:providerHigh,localUpper:totals.localUpper,localReported:totals.reported,localEstimated:totals.estimated,reserved:totals.reserved,effectiveUsed,remaining:Math.max(0,Math.min(plan.credits-effectiveUsed,account.remainingCredits-totals.reserved)),periodStart:account.periodStart,periodEnd:account.periodEnd,checkedAt:account.checkedAt,estimate:true,minimumReserve:FIRECRAWL_MAX_SCRAPE_CREDITS};
}
async function currentSession(c){const latest=await firecrawlSettings(c.env.DB);if(latest.revision!==c.s.revision||latest.mode!==c.s.mode||!latest.enabled)throw fcError('profile_changed',409);}
function validateScrape(c,url,options){
 const hosts=options.hosts;safeListingUrl(url,hosts);const host=new URL(url).hostname;
 if(/(^|\.)(?:linkedin\.com|x\.com|twitter\.com)$/.test(host))throw fcError('policy',400);
 if(Object.keys(options).some(k=>!['hosts','html','markdownOnly','onlyMainContent','timeoutMs','proxy','enhancedAllowed','signal','budget'].includes(k))||options.html!==undefined&&typeof options.html!=='boolean')throw fcError('policy',400);
 if(options.onlyMainContent!==undefined&&typeof options.onlyMainContent!=='boolean')throw fcError('policy',400);
 if(options.markdownOnly!==undefined&&typeof options.markdownOnly!=='boolean')throw fcError('policy',400);
 const proxy=options.proxy??'basic';if(proxy==='auto'||proxy==='enhanced'&&(c.s.mode!=='hobby'||!c.s.enhanced_allowed||!options.enhancedAllowed))throw fcError('policy',400);
 if(!['basic','enhanced'].includes(proxy))throw fcError('policy',400);
 if(options.timeoutMs!==undefined&&options.timeoutMs!==c.s.timeout_ms)throw fcError('policy',400);
}
async function refreshForScrape(c){
 await currentSession(c);let account;
 try{const response=await rawUsage(c.key,{signal:c.signal,timeoutMs:c.s.timeout_ms,retry:false});account=parseCreditUsage(response.account);}
 catch(e){const error=e instanceof FirecrawlError?e:fcError('usage_unavailable',503),cols=profileColumns(c.s.mode);await c.env.DB.prepare(`UPDATE firecrawl_settings SET ${cols.usage}='',${cols.status}='error',${cols.error}=? WHERE id=1 AND revision=?`).bind(error.message,c.s.revision).run();throw error;}
 if(![1000,5000].includes(account.planCredits)||c.s.mode==='hobby'&&account.planCredits!==5000)throw fcError('hobby_access',400);
 if(account.hasBonusBalance)throw fcError('usage_unavailable',503);
 await recordFirecrawlAccount(c.env.DB,c.keyHash,account);const cols=profileColumns(c.s.mode);
 const updated=await c.env.DB.prepare(`UPDATE firecrawl_settings SET ${cols.hash}=?,${cols.usage}=?,${cols.status}='connected',${cols.error}='',${cols.checked}=? WHERE id=1 AND revision=? AND mode=?`).bind(c.keyHash,JSON.stringify(account),new Date(account.checkedAt).toISOString(),c.s.revision,c.s.mode).run();if(!updated.meta?.changes)throw fcError('profile_changed',409);return account;
}
async function claim(c,account){
 const db=c.env.DB,now=Date.now(),id=crypto.randomUUID(),reserve=FIRECRAWL_MAX_SCRAPE_CREDITS,plan=FIRECRAWL_PLANS[c.s.mode];
 const result=await db.prepare(`INSERT INTO firecrawl_usage_events(id,key_hash,profile,period_start,period_end,operation,reserved_credits,outcome,started_at,active_until,generation)
 SELECT ?,?,?,?,?,'scrape',?,'reserved',?,?,? WHERE
 EXISTS(SELECT 1 FROM firecrawl_settings WHERE id=1 AND revision=? AND mode=? AND enabled=1)
 AND (SELECT provider_used_high FROM firecrawl_usage_periods WHERE key_hash=? AND period_start=? AND period_end=?)
 +(SELECT coalesce(sum(coalesce(charged_credits,reserved_credits)),0) FROM firecrawl_usage_events WHERE period_start=? AND period_end=? AND (profile=? OR key_hash=?)) + ? <= ?
 AND ? >= ? + (SELECT coalesce(sum(reserved_credits),0) FROM firecrawl_usage_events WHERE key_hash=? AND outcome='reserved' AND active_until>?)
 AND (SELECT count(*) FROM firecrawl_usage_events WHERE started_at>?) < ?
 AND (SELECT count(*) FROM firecrawl_usage_events WHERE active_until>?) < 1`).bind(id,c.keyHash,c.s.mode,account.periodStart,account.periodEnd,reserve,now,now+360000,boundGeneration(db),c.s.revision,c.s.mode,c.keyHash,account.periodStart,account.periodEnd,account.periodStart,account.periodEnd,c.s.mode,c.keyHash,reserve,plan.credits,account.remainingCredits,reserve,c.keyHash,now,now-60000,plan.scrapePerMinute,now).run();
 if(result.meta?.changes)return id;
 await currentSession(c);
 const active=await db.prepare('SELECT count(*) n FROM firecrawl_usage_events WHERE active_until>?').bind(now).first();if(active.n)throw fcError('busy',409);
 const rate=await db.prepare('SELECT count(*) n FROM firecrawl_usage_events WHERE started_at>?').bind(now-60000).first();if(rate.n>=plan.scrapePerMinute)throw fcError('rate_limit',429);
 throw fcError(c.s.mode==='free'?'free_exhausted':'hobby_exhausted',402);
}
async function settle(c,id,result,error){
 const status=result?.providerStatus??error?.providerStatus??null,reported=result?.creditsReported??error?.creditsReported;
 const definiteFree=['auth','credits','rate'].includes(error?.code);
 const known=typeof reported==='number'&&Number.isFinite(reported)&&reported>=0?reported:definiteFree?0:null;
 const outcome=error?known===null?'uncertain':'error':known===null?'uncertain':'complete';
 // Unknown/abandoned reservations remain fully charged in upper accounting.
 // Restore cannot delete these operational rows; old facade must not write data.
 try{await c.env.DB.prepare('UPDATE firecrawl_usage_events SET charged_credits=?,outcome=?,finished_at=?,active_until=?,provider_status=?,error_code=? WHERE id=? AND key_hash=?').bind(known,outcome,Date.now(),error&&['timeout','network','response'].includes(error.code)?Date.now()+360000:0,status,error?.code??null,id,c.keyHash).run();}catch(e){if(e.code!=='restored')throw e;}
}
async function preflightRateAndConcurrency(c){
 await currentSession(c);const now=Date.now();
 const counts=await c.env.DB.prepare('SELECT count(*) FILTER(WHERE active_until>?) AS active,count(*) FILTER(WHERE started_at>?) AS recent FROM firecrawl_usage_events').bind(now,now-60000).first();
 if(counts.active)throw fcError('busy',409);if(counts.recent>=FIRECRAWL_PLANS[c.s.mode].scrapePerMinute)throw fcError('rate_limit',429);
}
export async function firecrawlPolicyScrape(session,url,options={}){
 const c=sessions.get(session);if(!c)throw fcError('policy',403);validateScrape(c,url,options);let requests=0,creditChecks=0,totalKnown=0,allKnown=true;const started=Date.now();
 for(let attempt=0;attempt<2;attempt++){
  const budget=options.budget;if(budget&&budget.remaining<1)throw fcError('rate_limit',429,{requests,creditChecks});
  let account,id;
  try{await preflightRateAndConcurrency(c);creditChecks++;account=await refreshForScrape(c);id=await claim(c,account);}catch(e){throw Object.assign(e,{requests,creditChecks,durationMs:Date.now()-started});}
  if(budget)budget.remaining--;requests++;
  let result,error;
  try{result=await rawScrape(c.key,url,{hosts:options.hosts,html:options.html,markdownOnly:options.markdownOnly,onlyMainContent:options.onlyMainContent,timeoutMs:c.s.timeout_ms,proxy:c.s.mode==='free'?'basic':options.proxy??'basic',enhancedAllowed:c.s.mode==='hobby'&&!!c.s.enhanced_allowed,signal:c.signal,retry:false,budget:{remaining:1}});}catch(e){error=e;}
  await settle(c,id,result,error);
  const reported=result?.creditsReported??error?.creditsReported;if(typeof reported==='number'){totalKnown+=reported;}else if(!['auth','credits','rate'].includes(error?.code))allKnown=false;
  const metrics={requests,creditChecks,durationMs:Date.now()-started,profile:c.s.mode,creditsReported:allKnown?totalKnown:null};
  if(!error)return {...result,...metrics};
  if(attempt===0&&[429,503].includes(error.providerStatus)&&(!budget||budget.remaining>0)){
   await new Promise(r=>setTimeout(r,500));await currentSession(c);continue;
  }
  throw Object.assign(error,metrics);
 }
}
