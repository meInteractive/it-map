import {firecrawlSettings,profileColumns} from '../repositories/firecrawl-settings.js';
import {FIRECRAWL_PLANS,FIRECRAWL_STATS_MAX_AGE_MS,firecrawlMode,FIRECRAWL_MAX_SCRAPE_CREDITS} from '../integrations/firecrawl-plans.js';
import {firecrawlBudgetSnapshot} from './firecrawl-usage-policy.js';
import {refreshFirecrawlAccount} from './firecrawl-account.js';
export async function readFirecrawlBudgetView(env,{mode}={}){
 const s=await firecrawlSettings(env.DB),selected=firecrawlMode(mode??s.mode),c=profileColumns(selected),limits=FIRECRAWL_PLANS[selected];let account;try{account=JSON.parse(s[c.usage]);}catch{}
 const now=Date.now(),fresh=!!account&&Number.isFinite(account.checkedAt)&&now-account.checkedAt<=FIRECRAWL_STATS_MAX_AGE_MS&&account.checkedAt<=now&&Date.parse(account.periodStart)<=now&&Date.parse(account.periodEnd)>now;
 const base={mode:selected,limits,connectionStatus:s[c.status],configured:!!s[c.key],checkedAt:account?.checkedAt??(s[c.checked]?Date.parse(s[c.checked]):null),fresh,accountPlanConfirmed:false,subscriptionManagedExternally:true,autoTopUpAllowed:false};
 if(!s[c.key])return {...base,status:'missing_key',reason:'Сохраните ключ выбранного профиля.',budget:null};
 if(!fresh||!s[c.hash])return {...base,status:'unknown',reason:s[c.error]||'Актуальная статистика недоступна. Обновите статистику перед сбором.',budget:null};
 const budget=await firecrawlBudgetSnapshot(env.DB,selected,s[c.hash],account);let reason='';
 if(account.hasBonusBalance||![1000,5000].includes(account.planCredits))reason='По API нельзя безопасно определить включённый бюджет. Затратные операции заблокированы.';
 else if(selected==='hobby'&&account.planCredits!==5000)reason='API не подтвердил пакет 5000 кредитов для Hobby.';
 else if(selected!==s.mode)reason='Этот профиль не выбран. Новые операции по нему запрещены.';
 else if(!s.enabled)reason='Firecrawl выключен.';
 else if(budget.remaining<FIRECRAWL_MAX_SCRAPE_CREDITS)reason=selected==='free'?'Бесплатный лимит Firecrawl исчерпан':'Лимит включённых кредитов Hobby исчерпан';
 return {...base,status:reason?'blocked':'available',reason,budget,accountPlanConfirmed:selected==='hobby'&&account.planCredits===5000};
}
export async function refreshFirecrawlBudgetView(env,mode,options){await refreshFirecrawlAccount(env,mode,options);return readFirecrawlBudgetView(env,{mode});}
