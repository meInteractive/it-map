// Official Actor/API/pricing verified 2026-10-17. No arbitrary Actor or API host.
export const APIFY_ACTOR='shahidirfan~hh-ru-job-scraper';
export const APIFY_PLANS=Object.freeze({free:{name:'Free',priceUsd:0,creditsUsd:5},starter:{name:'Starter',priceUsd:19,creditsUsd:19}});
export const APIFY_RUN_CAP_USD=0.25,APIFY_READ_RESERVE_USD=0.01;
const messages={apify_auth:'Apify: проверьте API-токен и его права.',apify_network:'Apify временно недоступен.',apify_timeout:'Apify не ответил вовремя.',apify_data:'Apify вернул неподдерживаемые данные.',apify_budget:'Недостаточно доступного бюджета Apify для нового запуска с учётом резервов. Сбор сохранён для продолжения.',apify_disabled:'Apify выключен или API-токен не настроен.',apify_changed:'Настройки Apify изменились. Обновите страницу.',apify_running:'Apify собирает вакансии. Результат будет получен следующим тиком.',apify_uncertain:'Статус запуска Apify неизвестен. Автоматический повтор отключён, чтобы не списать бюджет дважды.',apify_failed:'Actor Apify завершился с ошибкой. Проверьте запуск в кабинете Apify.',apify_scope:'Для Apify допустим только источник hh.ru с конкретным работодателем или вакансией.',apify_busy:'Другой запуск Apify ещё выполняется.'};
export class ApifyError extends Error {constructor(code,status=409){super(code==='apify_incomplete'?'Сборщик Apify завершился, но не вернул заголовок или полное описание вакансии.':messages[code]||'Не удалось выполнить запрос Apify.');this.name='ApifyError';this.code=code;this.status=status;}}
export const apifyError=(code,status)=>new ApifyError(code,status);
export function apifyProfile(value){if(!Object.hasOwn(APIFY_PLANS,value))throw apifyError('apify_data',400);return value;}
export async function apifyRequest(token,path,{method='GET',body,signal,maxBytes=4000000,text=false}={}){
 if(!/^\/(?:users\/me(?:\/limits|\/usage\/monthly)?|actors\/[A-Za-z0-9_~.-]+\/runs|actor-runs\/[A-Za-z0-9]+|datasets\/[A-Za-z0-9]+(?:\/items)?|key-value-stores\/[A-Za-z0-9]+\/records\/INPUT|logs\/[A-Za-z0-9]+)(?:\?|$)/.test(path)||/^\/(?:users|datasets|key-value-stores|logs)\//.test(path)&&method!=='GET'||text&&(!/^\/logs\/[A-Za-z0-9]+(?:\?|$)/.test(path)||method!=='GET'))throw apifyError('apify_scope',400);
 const timeout=AbortSignal.timeout(15000),combined=signal?AbortSignal.any([signal,timeout]):timeout;let response;
 try{response=await fetch('https://api.apify.com/v2'+path,{method,redirect:'manual',headers:{authorization:'Bearer '+token,'content-type':'application/json'},...(body?{body:JSON.stringify(body)}:{}),signal:combined});}
 catch{if(signal?.aborted)throw signal.reason;if(timeout.aborted)throw apifyError('apify_timeout',503);throw apifyError('apify_network',503);}
 if(!response.ok){await response.body?.cancel();throw apifyError(response.status===401||response.status===403?'apify_auth':response.status===402?'apify_budget':response.status===429?'apify_busy':response.status>=500?'apify_network':'apify_data',response.status===401||response.status===403?400:503);}
 const reader=response.body?.getReader();if(!reader)throw apifyError('apify_data',502);let size=0;const chunks=[];
 try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.length;if(size>maxBytes){await reader.cancel();throw apifyError('apify_data',502);}chunks.push(value);}}
 catch(e){if(e instanceof ApifyError)throw e;if(signal?.aborted)throw signal.reason;throw apifyError('apify_network',503);}
 const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.length;}
 const decoded=new TextDecoder().decode(bytes);if(text)return decoded;
 try{return JSON.parse(decoded);}catch{throw apifyError('apify_data',502);}
}
export function hhInput(sourceUrl,{page=0,detail=false,orderBy,titleQuery}={}){
 if(orderBy!==undefined&&orderBy!=='publication_time')throw apifyError('apify_data',400);
 if(titleQuery!==undefined&&titleQuery!==null&&(typeof titleQuery!=='string'||!titleQuery.startsWith('NAME:(')||titleQuery.length>2000||/[\u0000-\u001f]/.test(titleQuery)))throw apifyError('apify_data',400);
 let url;try{url=new URL(sourceUrl);}catch{throw apifyError('apify_scope',400);}
 if(url.protocol!=='https:'||!/(^|\.)hh\.ru$/.test(url.hostname)||url.username||url.password||url.port)throw apifyError('apify_scope',400);
 const vacancy=url.pathname.match(/^\/vacancy\/(\d+)\/?$/),employer=url.pathname.match(/^\/employer\/(\d+)\/?$/);
 if(detail&&!vacancy)throw apifyError('apify_scope',400);
 if(!vacancy){
  const employerId=employer?.[1]||url.searchParams.get('employer_id');if(!/^\d+$/.test(employerId||'')||!employer&&url.searchParams.getAll('employer_id').length!==1||!employer&&url.pathname!=='/search/vacancy')throw apifyError('apify_scope',400);
  if(employer){url=new URL('https://hh.ru/search/vacancy');url.searchParams.set('employer_id',employerId);}
  url.hostname='hh.ru';url.searchParams.set('page',String(page));url.searchParams.set('per_page','100');
  if(orderBy&&!url.searchParams.has('order_by'))url.searchParams.set('order_by',orderBy);
  if(titleQuery){const old=url.searchParams.get('text');url.searchParams.set('text',old?'('+old+') AND ('+titleQuery+')':titleQuery);}
 }
 url.hash='';
 return {startUrl:url.toString(),results_wanted:vacancy?1:100,max_pages:1,api_only:!vacancy,include_raw_data:false,proxyConfiguration:{useApifyProxy:true}};
}
