export interface OpenAIPricing {
 inputUsdPerMillion:number;outputUsdPerMillion:number;currency:'USD';tier:'standard';verifiedAt:string;source:string;
}
export interface OpenRouterPricing {prompt:number|string;completion?:number|string|null;request?:number|string|null;}
export const RUB_PER_USD=85,ESTIMATE_GENERATIONS=1000,CHARS_PER_TOKEN=2;
export function formatUsd(value:number){return '$'+value.toLocaleString('ru-RU',{minimumFractionDigits:2,maximumFractionDigits:6});}
export function formatRub(value:number){return value.toLocaleString('ru-RU',{minimumFractionDigits:2,maximumFractionDigits:2})+' ₽';}
export function outputPrice(pricing?:OpenAIPricing|null){return pricing?formatUsd(pricing.outputUsdPerMillion)+' / 1 млн выходных токенов':'Цена не подтверждена';}
function estimate(inputTokens:number,outputTokens:number,inputUsdPerToken:number,outputUsdPerToken:number,requestUsd=0){
 const inputRub=inputTokens*inputUsdPerToken*ESTIMATE_GENERATIONS*RUB_PER_USD;
 const outputRub=outputTokens*outputUsdPerToken*ESTIMATE_GENERATIONS*RUB_PER_USD;
 const requestRub=requestUsd*ESTIMATE_GENERATIONS*RUB_PER_USD;
 return {inputTokens,outputTokens,inputRub,outputRub,requestRub,totalRub:inputRub+outputRub+requestRub,generations:ESTIMATE_GENERATIONS,rubPerUsd:RUB_PER_USD};
}
// Planning heuristic, not a tokenizer. Extra reasoning tokens are not predictable.
export function letterCost(pricing?:OpenAIPricing|null){
 if(!pricing)return null;
 return estimate(20000/CHARS_PER_TOKEN,5000/CHARS_PER_TOKEN,pricing.inputUsdPerMillion/1e6,pricing.outputUsdPerMillion/1e6);
}
export function relevanceCost(pricing?:OpenAIPricing|null){
 if(!pricing)return null;
 return estimate(15000/CHARS_PER_TOKEN,1,pricing.inputUsdPerMillion/1e6,pricing.outputUsdPerMillion/1e6);
}
const price=(value:number|string|null|undefined)=>(typeof value==='number'||(typeof value==='string'&&value.trim()!==''))&&Number.isFinite(Number(value))&&Number(value)>=0?Number(value):null;
export function openRouterRelevanceCost(pricing?:OpenRouterPricing|null){
 if(!pricing)return null;
 const prompt=price(pricing.prompt),completion=pricing.completion==null?0:price(pricing.completion),request=pricing.request==null?0:price(pricing.request);
 if(prompt==null||completion==null||request==null)return null;
 return estimate(15000/CHARS_PER_TOKEN,1,prompt,completion,request);
}
