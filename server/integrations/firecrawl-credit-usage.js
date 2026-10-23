import {fcError} from './firecrawl-transport.js';
export function parseCreditUsage(account,now=Date.now()){
 const start=Date.parse(account?.billingPeriodStart),end=Date.parse(account?.billingPeriodEnd),plan=account?.planCredits,left=account?.remainingCredits;
 if(!Number.isSafeInteger(plan)||plan<1||!Number.isSafeInteger(left)||left<0||!Number.isFinite(start)||!Number.isFinite(end)||start>now||end<=now||end-start>35*86400000||end-start<86400000)throw fcError('usage_unavailable',503);
 return {planCredits:plan,remainingCredits:left,periodStart:new Date(start).toISOString(),periodEnd:new Date(end).toISOString(),providerUsedEstimate:Math.max(0,plan-left),hasBonusBalance:left>plan,checkedAt:now};
}
