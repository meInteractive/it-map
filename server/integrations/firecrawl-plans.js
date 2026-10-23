// Verified official billing/rate-limits/pricing on 2026-10-14. App caps, not a
// subscription switch. Keep transport concurrency=1 (stricter than both plans).
export const FIRECRAWL_PLANS=Object.freeze({
 free:Object.freeze({name:'Free',credits:1000,scrapePerMinute:10,browsers:2,appConcurrency:1,priceUsd:0}),
 hobby:Object.freeze({name:'Hobby',credits:5000,scrapePerMinute:100,browsers:5,appConcurrency:1,priceUsd:19})
});
export const FIRECRAWL_PLAN_VERIFIED_AT='2026-10-14';
export const FIRECRAWL_MAX_SCRAPE_CREDITS=4; // base1 + possible team scan2 + ZDR1; no AI options/provider-routed domains
export const FIRECRAWL_STATS_MAX_AGE_MS=60000;
export const firecrawlMode=value=>{if(!Object.hasOwn(FIRECRAWL_PLANS,value))throw Object.assign(new Error('Режим Firecrawl: free или hobby.'),{status:400});return value;};
