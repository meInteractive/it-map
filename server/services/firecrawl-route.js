import {parserOwner} from '../repositories/parser-owner.js';
import {ApifyError} from '../integrations/apify.js';
import {enqueueSavedCandidates,startClassificationJobs} from './classification-jobs.js';
import {readFirecrawlBudgetView,refreshFirecrawlBudgetView} from './firecrawl-budget-view.js';
import {saveFirecrawlMode,refreshFirecrawlAccount} from './firecrawl-account.js';
import {readVacancyParsingSources,saveVacancyParsingSource} from '../repositories/vacancy-source-parsing.js';
import {fetchVacancyListing,readVacancyDiscoveryRun,saveVacancyDiscoveryRun,resumeVacancyDiscoveryRun,listPausedVacancyDiscoveryRuns} from './vacancy-discovery.js';
import {jsonResponse} from '../http.js';
import {candidateDb} from '../repositories/vacancy-candidates.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
import {firecrawlSettings,publicFirecrawlSettings,firecrawlKey,saveFirecrawlKey,saveFirecrawlSettings,withFirecrawlLease} from '../repositories/firecrawl-settings.js';
import {testFirecrawlConnection,FirecrawlError,fcError} from '../integrations/firecrawl.js';
export async function firecrawlRoute(request,env,url,user,ctx){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
 if(!env?.DB)return jsonResponse({error:'Хранилище пока не подключено.'},503);
 if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
  env={...env,DB:await candidateDb(env.DB)};const s=await firecrawlSettings(env.DB),path=url.pathname.slice('/api/firecrawl'.length);
  if(path==='/settings'&&request.method==='GET')return jsonResponse(publicFirecrawlSettings(s));
  if(path==='/settings'&&request.method==='PUT')return jsonResponse(await saveFirecrawlSettings(env.DB,await readClassificationInput(request)));
  if(path==='/mode'&&request.method==='PUT')return jsonResponse(await saveFirecrawlMode(env,await readClassificationInput(request),{signal:request.signal}));
  if(path==='/key'&&request.method==='PUT')return jsonResponse(await saveFirecrawlKey(env,await readClassificationInput(request)));
  if(path==='/check'&&request.method==='POST'){
   const input=await readClassificationInput(request);if(Object.keys(input).some(k=>k!=='mode')||input.mode!==undefined&&!['free','hobby'].includes(input.mode))throw Object.assign(new Error('Передайте mode: free или hobby.'),{status:400});
   return jsonResponse(await withFirecrawlLease(env.DB,async assertOwned=>{
    const {account}=await refreshFirecrawlAccount(env,input.mode??s.mode,{signal:request.signal});await assertOwned();return {ok:true,remainingCredits:account.remainingCredits,account,state:publicFirecrawlSettings(await firecrawlSettings(env.DB))};
   }));
  }
  if(path==='/usage'&&request.method==='GET'){
   if([...url.searchParams.keys()].some(k=>k!=='mode'))throw Object.assign(new Error('Неизвестный параметр.'),{status:400});return jsonResponse(await readFirecrawlBudgetView(env,{mode:url.searchParams.get('mode')??undefined}));
  }
  if(path==='/usage/refresh'&&request.method==='POST'){
   const input=await readClassificationInput(request);if(Object.keys(input).some(k=>k!=='mode'))throw Object.assign(new Error('Передайте mode.'),{status:400});return jsonResponse(await withFirecrawlLease(env.DB,()=>refreshFirecrawlBudgetView(env,input.mode??s.mode,{signal:request.signal})));
  }
  if(path==='/pending'&&request.method==='GET'){
   if([...url.searchParams.keys()].some(k=>!['companyId','agencyId','sourceId'].includes(k)))throw Object.assign(new Error('Неизвестный фильтр.'),{status:400});return jsonResponse(await listPausedVacancyDiscoveryRuns(env.DB,user.name,Object.fromEntries(url.searchParams)));
  }
  if(path==='/sources'&&request.method==='GET'){
   if([...url.searchParams.keys()].some(k=>!['companyId','agencyId'].includes(k)))throw Object.assign(new Error('Неизвестный параметр.'),{status:400});
   return jsonResponse({sources:await readVacancyParsingSources(env.DB,{companyId:url.searchParams.get('companyId')??undefined,agencyId:url.searchParams.get('agencyId')??undefined,favoritesOnly:s.favorites_only!==0,userName:user.name})});
  }
  const agencySource=path.match(/^\/agency-sources\/(\d+)\/([A-Za-z]+)$/);
  if(agencySource&&request.method==='PUT')return jsonResponse(await saveVacancyParsingSource(env.DB,{agencyId:Number(agencySource[1])},agencySource[2],await readClassificationInput(request)));
  const sourceMatch=path.match(/^\/sources\/(\d+)\/([A-Za-z]+)$/);
  if(sourceMatch&&request.method==='PUT')return jsonResponse(await saveVacancyParsingSource(env.DB,Number(sourceMatch[1]),sourceMatch[2],await readClassificationInput(request)));
  if(path==='/test'&&request.method==='POST'){
   const input=await readClassificationInput(request);if(Object.keys(input).some(k=>!['companyId','agencyId','sourceId'].includes(k))||typeof input.sourceId!=='string')throw Object.assign(new Error('Передайте companyId и sourceId.'),{status:400});
   return jsonResponse(await fetchVacancyListing(env,parserOwner(input),input.sourceId,{userName:user.name,signal:request.signal}));
  }
  const runMatch=path.match(/^\/runs\/([0-9a-f-]{36})(?:\/(save|resume))?$/);
  if(runMatch&&!runMatch[2]&&request.method==='GET')return jsonResponse((await readVacancyDiscoveryRun(env.DB,runMatch[1],user.name)).result);
  if(runMatch&&runMatch[2]&&request.method==='POST'){
   if(Object.keys(await readClassificationInput(request)).length)throw Object.assign(new Error('Передайте только ID серверного результата.'),{status:400});
   if(runMatch[2]==='resume')return jsonResponse(await resumeVacancyDiscoveryRun(env,runMatch[1],user.name,{signal:request.signal}));
   const result=await saveVacancyDiscoveryRun(env.DB,runMatch[1],user.name);
   try{const ids=result.jobs.filter(j=>['new','existing'].includes(j.saveState)&&j.existingId).map(j=>j.existingId);result.classification=await enqueueSavedCandidates(env,ids,runMatch[1]);result.classification.execution=startClassificationJobs(env,ctx,result.classification.jobIds);}catch{result.classification={jobIds:[],queued:0,reused:0,error:'Вакансии сохранены. Не удалось поставить классификацию в очередь; запустите её в настройках.'};}
   return jsonResponse(result);
  }
  return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e instanceof FirecrawlError||e instanceof ApifyError||[400,403,404,422].includes(e.status)||e.code==='restored'?e.message:'Не удалось выполнить операцию Firecrawl.',code:e.code||'input',providerStatus:e.providerStatus??null,targetStatus:e.targetStatus??null},e.status||500);}
}
