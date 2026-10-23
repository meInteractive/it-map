// Standard, uncached text tokens, USD per million. Verified 2026-10-09.
// Availability still comes from /v1/models; it does not publish prices.
// Never infer a tariff for fine-tunes, new variants or dated snapshots.
export const OPENAI_PRICING_DATE='2026-10-09';
export const OPENAI_PRICING_SOURCE='https://developers.openai.com/api/docs/pricing';
const rates={
 'gpt-6-luna':[0.10,0.50],
 'gpt-6.1-sol':[2,10],
 'gpt-6-sol':[2,10],
 'gpt-6-astra':[10,50],
 'gpt-5.6-terra':[2,12],
 'gpt-5.6-luna':[0.20,1.20],
 'gpt-5.6-sol':[4,20],
 'gpt-5.6-cyber':[12.50,75],
 'gpt-5.4':[2.50,15],
 'gpt-5.4-mini':[0.75,4.50],
 'gpt-5.4-nano':[0.20,1.25],
 'gpt-5.4-pro':[30,180],
 'gpt-5.3-codex':[1.75,14],
 'gpt-5.2':[1.75,14],
 'gpt-5.2-pro':[21,168],
 'gpt-5.1':[1.25,10],
 'gpt-5':[1.25,10],
 'gpt-5-mini':[0.25,2],
 'gpt-5-nano':[0.05,0.40],
 'gpt-4.1':[2,8],
 'gpt-4.1-mini':[0.40,1.60],
 'gpt-4.1-nano':[0.10,0.40],
 'gpt-4o':[2.50,10],
 'gpt-4o-mini':[0.15,0.60],
 'o3':[2,8],
 'o3-mini':[1.10,4.40],
 'o3-pro':[20,80],
 'o4-mini':[1.10,4.40],
 'o1':[15,60],
 'o1-mini':[1.10,4.40],
 'o1-pro':[150,600],
};
// Official model page explicitly identifies this alias.
const aliases={'gpt-5.6':'gpt-5.6-sol'};
export function openaiPricing(id){
 const model=Object.hasOwn(aliases,id)?aliases[id]:id;
 if(!Object.hasOwn(rates,model))return null;
 const [inputUsdPerMillion,outputUsdPerMillion]=rates[model];
 return {inputUsdPerMillion,outputUsdPerMillion,currency:'USD',tier:'standard',verifiedAt:OPENAI_PRICING_DATE,source:model==='gpt-5.6-cyber'?OPENAI_PRICING_SOURCE:'https://developers.openai.com/api/docs/models/'+model};
}
