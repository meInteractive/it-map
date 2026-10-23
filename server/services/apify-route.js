import {operationErrorCode} from './operation-error.js';
import {jsonResponse} from '../http.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
import {apifySettings,publicApifySettings,saveApifyKey,saveApifySettings} from '../repositories/apify-settings.js';
import {ApifyError} from '../integrations/apify.js';
import {refreshApifyAccount,apifyBudget} from './apify-account.js';
import {reconcileApifyRuns} from './apify-runs.js';
export async function apifyRoute(request,env,url,user){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
 if(!env?.DB)return jsonResponse({error:'Хранилище пока не подключено.'},503);
 if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  env={...env,DB:await candidateDb(env.DB)};const s=await apifySettings(env.DB),path=url.pathname.slice('/api/apify'.length);
  if(path==='/settings'&&request.method==='GET')return jsonResponse(publicApifySettings(s));
  if(path==='/key'&&request.method==='PUT')return jsonResponse(await saveApifyKey(env,await readClassificationInput(request)));
  if(path==='/settings'&&request.method==='PUT')return jsonResponse(await saveApifySettings(env.DB,await readClassificationInput(request)));
  if(path==='/check'&&request.method==='POST'){const input=await readClassificationInput(request);if(Object.keys(input).some(k=>k!=='profile')||!['free','starter'].includes(input.profile))throw new ApifyError('apify_data',400);const reconciliation=await reconcileApifyRuns(env,{profile:input.profile,signal:request.signal});return jsonResponse({...await refreshApifyAccount(env,input.profile,{signal:request.signal}),reconciliation});}
  if(path==='/usage'&&request.method==='GET'){
   const profiles={};for(const profile of ['free','starter']){let account=null;try{account=JSON.parse(s[profile+'_account_json']);}catch{}profiles[profile]=account?await apifyBudget(env.DB,profile,account):null;}
   const blocked=await env.DB.prepare("SELECT id,profile,state,run_id,created_at FROM apify_runs WHERE state IN ('starting','running','uncertain') ORDER BY created_at DESC LIMIT 5").all();return jsonResponse({profiles,active:blocked.results||[]});
  }
  return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e instanceof ApifyError||e.code==='restored'?e.message:'Не удалось выполнить операцию Apify.',code:operationErrorCode(e)},e.status||500);}
}
