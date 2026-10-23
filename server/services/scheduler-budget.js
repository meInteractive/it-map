import {schedulerSettings,schedulerError} from '../repositories/scheduler.js';
import {openaiPricing} from '../integrations/openai-pricing.js';
export function schedulerPeriod(now=Date.now()){
 const d=new Date(now+10800000);return {day:Date.UTC(d.getUTCFullYear(),d.getUTCMonth(),d.getUTCDate())-10800000,month:Date.UTC(d.getUTCFullYear(),d.getUTCMonth(),1)-10800000};
}
export function schedulerUpperCost(request){
 if(request.free===true)return 0;
 if(request.provider==='apify'&&request.capUsd===0.26)return 0.26;
 if(request.provider==='openai'){
  const p=openaiPricing(request.model);if(!p)throw schedulerError('budget_unknown','Нет проверенного тарифа выбранной модели OpenAI. Платный запрос отложен.');
  const bytes=new TextEncoder().encode(JSON.stringify(request.payload)).length+4096+(request.payload.input?.length||0)*256;
  const output=request.payload.max_output_tokens;if(!Number.isSafeInteger(output)||output<1)throw schedulerError('budget_unknown','Нет верхней границы выходных токенов.');
  return (bytes*p.inputUsdPerMillion+output*p.outputUsdPerMillion)/1e6;
 }
 const p=request.pricing,context=Number(request.contextLength),prompt=Number(p?.prompt),completion=Number(p?.completion),fixed=Number(p?.request??0);
 if(!Number.isFinite(context)||context<=0||context>10000000||!p||p.prompt===undefined||p.completion===undefined||![prompt,completion,fixed].every(n=>Number.isFinite(n)&&n>=0))throw schedulerError('budget_unknown','Нет надёжной верхней оценки стоимости JEV. Запрос отложен.');
 // Bound every independent choice by the model's complete context, including
 // completion/reasoning, rather than guessing title token count.
 const questions=Object.keys(request.payload.questions||{}).length+1;
 return (context*(prompt+completion)*2+fixed)*questions;
}
export function schedulerBilling(db,attemptId,assertActive){
 let unknown=false,requests=0;
 return {
  get unknown(){return unknown},get requests(){return requests},
  async run(request,work){
   await assertActive();const upper=schedulerUpperCost(request),now=Date.now(),period=schedulerPeriod(now);
   const claim=await db.prepare(`UPDATE scheduler_attempts SET reserved_usd=reserved_usd+?,provider=?,model=? WHERE id=? AND (?=0 OR (SELECT coalesce(sum(coalesce(cost_usd,reserved_usd)),0) FROM scheduler_attempts WHERE started_at>=?)+?<=coalesce((SELECT json_extract(scheduler_json,'$.dayBudgetUsd') FROM firecrawl_settings WHERE id=1),0)) AND (?=0 OR (SELECT coalesce(sum(coalesce(cost_usd,reserved_usd)),0) FROM scheduler_attempts WHERE started_at>=?)+?<=coalesce((SELECT json_extract(scheduler_json,'$.monthBudgetUsd') FROM firecrawl_settings WHERE id=1),0))`).bind(upper,request.provider,request.model,attemptId,upper,period.day,upper,upper,period.month,upper).run();
   if(!claim.meta?.changes)throw schedulerError('scheduler_budget','Дневной или месячный бюджет автоматического парсера исчерпан.');
   requests++;let response;
   try{await assertActive();response=await work();}catch(e){unknown=true;throw e;}
   const usage=response.usage||{};let cost=typeof usage.cost==='number'&&Number.isFinite(usage.cost)&&usage.cost>=0?usage.cost:null;
   if(cost===null&&request.free===true)cost=0;
   if(cost===null&&request.provider==='openai'){
    const p=openaiPricing(response.model||request.model),input=usage.input_tokens,output=usage.output_tokens;
    if(p&&Number.isFinite(input)&&input>=0&&Number.isFinite(output)&&output>=0&&!usage.input_tokens_details?.cached_tokens)cost=(input*p.inputUsdPerMillion+output*p.outputUsdPerMillion)/1e6;
   }
   if(cost===null)unknown=true;
   else await db.prepare('UPDATE scheduler_attempts SET reserved_usd=MAX(0,reserved_usd-?)+? WHERE id=?').bind(upper,cost,attemptId).run();
   return response;
  }
 };
}
export async function schedulerSpending(db){const p=schedulerPeriod(),rows=(await db.prepare("SELECT type,sum(coalesce(cost_usd,reserved_usd)) usd,sum(CASE WHEN cost_usd IS NULL AND reserved_usd>0 THEN 1 ELSE 0 END) unknown,sum(credits) credits FROM scheduler_attempts WHERE started_at>=? GROUP BY type").bind(p.month).all()).results||[];const day=(await db.prepare('SELECT coalesce(sum(coalesce(cost_usd,reserved_usd)),0) usd FROM scheduler_attempts WHERE started_at>=?').bind(p.day).first()).usd;return {dayUsd:day,monthUsd:rows.reduce((n,r)=>n+r.usd,0),byType:rows,timezone:'Europe/Moscow'};}
