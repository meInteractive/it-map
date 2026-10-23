import {parseCreditUsage} from '../integrations/firecrawl-credit-usage.js';
import {recordFirecrawlAccount} from './firecrawl-usage-policy.js';
import {firecrawlSettings,firecrawlKey,profileColumns,publicFirecrawlSettings} from '../repositories/firecrawl-settings.js';
import {testFirecrawlConnection,fcError,FirecrawlError} from '../integrations/firecrawl.js';
import {firecrawlMode} from '../integrations/firecrawl-plans.js';
import {candidateHash} from '../repositories/vacancy-candidates.js';
export {parseCreditUsage} from '../integrations/firecrawl-credit-usage.js';
export async function refreshFirecrawlAccount(env,mode,{signal}={}){
 firecrawlMode(mode);const s=await firecrawlSettings(env.DB),c=profileColumns(mode),key=await firecrawlKey(env,s,mode),hash=await candidateHash(key);let account;
 try{const r=await testFirecrawlConnection(env,{mode,signal});account=parseCreditUsage(r.account);const result=await env.DB.prepare(`UPDATE firecrawl_settings SET ${c.hash}=?,${c.usage}=?,${c.status}='connected',${c.error}='',${c.checked}=? WHERE id=1 AND revision=?`).bind(hash,JSON.stringify(account),new Date(account.checkedAt).toISOString(),s.revision).run();if(!result.meta?.changes)throw fcError('changed',409);await recordFirecrawlAccount(env.DB,hash,account);return {account,keyHash:hash,snapshot:s};}
 catch(e){if(e.code!=='changed'&&e.code!=='restored')await env.DB.prepare(`UPDATE firecrawl_settings SET ${c.status}='error',${c.error}=?,${c.usage}='',${c.checked}=? WHERE id=1 AND revision=?`).bind(e instanceof FirecrawlError?e.message:'Статистика недоступна.',new Date().toISOString(),s.revision).run();throw e;}
}
export async function saveFirecrawlMode(env,input,{signal}={}){
 if(Object.keys(input).some(k=>!['mode','revision','confirmed'].includes(k))||!Number.isSafeInteger(input.revision))throw Object.assign(new Error('Передайте mode, revision и confirmed для Hobby.'),{status:400});const mode=firecrawlMode(input.mode),s=await firecrawlSettings(env.DB);if(s.revision!==input.revision)throw fcError('changed',409);
 if(mode==='hobby'){if(input.confirmed!==true)throw fcError('confirmation',400);const {account}=await refreshFirecrawlAccount(env,'hobby',{signal});if(account.planCredits!==5000)throw fcError('hobby_access',400);}
 const r=await env.DB.prepare("UPDATE firecrawl_settings SET mode=?,proxy='basic',enhanced_allowed=0,enabled=CASE WHEN ?='free' AND length(key_encrypted)=0 THEN 0 ELSE enabled END,revision=revision+1 WHERE id=1 AND revision=?").bind(mode,mode,s.revision).run();if(!r.meta?.changes)throw fcError('changed',409);return publicFirecrawlSettings(await firecrawlSettings(env.DB));
}
