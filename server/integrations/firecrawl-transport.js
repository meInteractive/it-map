// Ordinary REST Scrape only. Never accept arbitrary endpoint/options from UI.
export const FIRECRAWL_API='https://api.firecrawl.dev/v2/';
const messages={usage_unavailable:'Не удалось проверить расчётный период и доступный бюджет Firecrawl. Затратные запросы запрещены.',hobby_access:'API не подтвердил доступ к включённому пакету 5000 кредитов Hobby. Проверьте ключ и подписку.',confirmation:'Для включения Hobby нужно явное подтверждение.',profile_changed:'Режим или ключ Firecrawl изменён. Новые запросы остановлены; отправленный запрос может продолжать расходовать кредиты.',free_exhausted:'Бесплатный лимит Firecrawl исчерпан.',hobby_exhausted:'Лимит включённых кредитов Hobby исчерпан.',policy:'Операция или её стоимость не разрешена выбранным профилем Firecrawl.',rate_limit:'Достигнут минутный лимит Firecrawl. Новые запросы пока не отправляются.',missing_key:'Сохраните API-ключ Firecrawl.',auth:'Firecrawl отклонил ключ.',credits:'Недостаточно кредитов Firecrawl.',rate:'Превышен лимит запросов Firecrawl. Повторите позже.',timeout:'Истекло время ожидания Firecrawl.',network:'Не удалось связаться с Firecrawl.',provider:'Ошибка API Firecrawl.',target:'Целевая страница недоступна.',blocked:'Источник ограничивает автоматизированный доступ. Обход не выполняется.',redirect:'Перенаправление за пределы разрешённых доменов.',unsafe_url:'Недопустимый адрес источника.',response:'Некорректный или слишком большой ответ Firecrawl.',busy:'Уже выполняется запрос Firecrawl. Дождитесь его завершения.',changed:'Настройки или источник изменились. Повторите проверку.',disabled:'Включите Firecrawl и парсинг выбранного источника.',listing_unavailable:'Площадка не смогла загрузить данные списка вакансий. Полнота не подтверждена; следующая проверка по выбранному интервалу.',extraction:'Не удалось надёжно выделить заголовки и ссылки. Настройте правило источника.',expired:'Результат проверки устарел. Запустите новую проверку.'};
export class FirecrawlError extends Error{constructor(code,status=502,details={}){super(messages[code]||messages.provider);this.code=code;this.status=status;Object.assign(this,details);}}
export const fcError=(code,status=502,details)=>new FirecrawlError(code,status,details);
export function publicHost(host){
 const h=host.toLowerCase().replace(/\.$/,'');
 if(h.length>253||!h.split('.').every(label=>/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label))||!h||!h.includes('.')||h.includes(':')||h.startsWith('[')||/(^|\.)(localhost|local|internal|lan|test|invalid|example|onion)$/.test(h)||/^(metadata|metadata\.google\.internal)$/.test(h))return false;
 // URL normalizes octal/hex/short IPv4 forms before this check. All literal
 // IPs are rejected: configured job sources must use public DNS hostnames.
 return !/^\d+(?:\.\d+){3}$/.test(h);
}
export function safeListingUrl(value,allowedHosts){
 let u;try{u=new URL(value);}catch{throw fcError('unsafe_url',400);}
 if(!['http:','https:'].includes(u.protocol)||u.username||u.password||u.port||!publicHost(u.hostname)||u.href.length>2048||/[\u0000-\u001f]/.test(value))throw fcError('unsafe_url',400);
 if(allowedHosts&&!allowedHosts.includes(u.hostname.toLowerCase()))throw fcError('redirect',400);
 u.hash='';return u.href;
}
const publicIp=ip=>{
 if(ip.includes(':'))return !/^(::|0:|fe[89ab]|f[cd]|ff|2001:db8|2001:0:|2002:|64:ff9b)/i.test(ip)&&!ip.includes('.');
 const a=ip.split('.').map(Number);return a.length===4&&a.every(n=>Number.isInteger(n)&&n>=0&&n<=255)&&!(a[0]===0||a[0]===10||a[0]===127||a[0]>=224||a[0]===169&&a[1]===254||a[0]===172&&a[1]>=16&&a[1]<=31||a[0]===192&&(a[1]===168||a[1]===0||a[1]===2)||a[0]===100&&a[1]>=64&&a[1]<=127||a[0]===198&&(a[1]===18||a[1]===19||a[1]===51)||a[0]===203&&a[1]===0&&a[2]===113);
};
// Resolve both address families and inspect redirects BEFORE sending to provider.
// The provider independently resolves DNS; final metadata is checked as well.
export async function preflightListingUrl(url,hosts,{signal,fetcher=fetch}={}){
 let current=safeListingUrl(url,hosts);
 for(let hop=0;hop<4;hop++){
  const host=new URL(current).hostname;let addresses=[];
  for(const type of ['A','AAAA']){
   let r,j;try{r=await fetcher('https://cloudflare-dns.com/dns-query?name='+encodeURIComponent(host)+'&type='+type,{headers:{accept:'application/dns-json'},signal:signal?AbortSignal.any([signal,AbortSignal.timeout(8000)]):AbortSignal.timeout(8000)});j=await r.json();}catch{throw fcError('network');}
   if(!r.ok||![0,3].includes(j.Status))throw fcError('network');addresses.push(...(j.Answer||[]).filter(a=>[1,28].includes(a.type)).map(a=>a.data));
  }
  if(!addresses.length||addresses.some(a=>!publicIp(a)))throw fcError('unsafe_url',400);
  let r;try{r=await fetcher(current,{method:'HEAD',redirect:'manual',signal:signal?AbortSignal.any([signal,AbortSignal.timeout(10000)]):AbortSignal.timeout(10000)});}catch{throw fcError('network');}
  if(r.status>=300&&r.status<400&&r.headers.get('location')){current=safeListingUrl(new URL(r.headers.get('location'),current).href,hosts);continue;}
  if([401,403,429].includes(r.status))throw fcError('blocked',422,{targetStatus:r.status});
  if(r.status>=400&&r.status!==405)throw fcError('target',422,{targetStatus:r.status});
  return current;
 }
 throw fcError('redirect',422);
}
async function boundedJson(response){
 const reader=response.body?.getReader();if(!reader)throw fcError('response');let size=0,parts=[];
 while(true){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>2*1024*1024){await reader.cancel();throw fcError('response');}parts.push(value);}
 const bytes=new Uint8Array(size);let at=0;for(const p of parts){bytes.set(p,at);at+=p.length;}
 try{return JSON.parse(new TextDecoder().decode(bytes));}catch{throw fcError('response');}
}
export async function firecrawlRequest(key,path,{body,timeoutMs=45000,signal,fetcher=fetch,budget={remaining:1},retry=true}={}){
 if(!key)throw fcError('missing_key',400);let requests=0;const start=Date.now();
 for(let attempt=0;attempt<2;attempt++){
  if(budget.remaining<=0)throw fcError('rate',429,{requests,durationMs:Date.now()-start});budget.remaining--;requests++;
  let response,data;
  try{response=await fetcher(FIRECRAWL_API+path,{method:body?'POST':'GET',headers:{authorization:'Bearer '+key,'content-type':'application/json'},...(body?{body:JSON.stringify(body)}:{}),redirect:'manual',signal:signal?AbortSignal.any([signal,AbortSignal.timeout(timeoutMs+2000)]):AbortSignal.timeout(timeoutMs+2000)});data=await boundedJson(response);}
  catch(e){if(e instanceof FirecrawlError)throw Object.assign(e,{requests,durationMs:Date.now()-start});throw fcError(e?.name==='TimeoutError'||e?.name==='AbortError'?'timeout':'network',502,{requests,durationMs:Date.now()-start});}
  if(response.ok&&data.success===true)return {data:data.data,requests,durationMs:Date.now()-start,providerStatus:response.status};
  const status=response.status,code=status===401?'auth':status===402?'credits':status===403?'blocked':status===429?'rate':/timeout/i.test(String(data.code))?'timeout':'provider';
  // One retry for an explicit rate/service response; uncertain POST timeouts
  // are not replayed because provider may already have charged the scrape.
  if(retry&&attempt===0&&budget.remaining>0&&(status===429||status===503)){
   const seconds=Number(response.headers.get('retry-after'));if(Number.isFinite(seconds)&&seconds>2)throw fcError(code,status,{providerStatus:status,requests,durationMs:Date.now()-start});
   await new Promise(resolve=>setTimeout(resolve,Math.max(500,seconds*1000||500)));signal?.throwIfAborted();continue;
  }
  throw fcError(code,status===401?400:status===429?429:502,{providerStatus:status,requests,durationMs:Date.now()-start});
 }
}
export async function firecrawlScrape(key,url,{hosts,html=false,markdownOnly=false,onlyMainContent=true,timeoutMs=45000,proxy='basic',enhancedAllowed=false,...rest}={}){
 safeListingUrl(url,hosts);if(!['basic','enhanced'].includes(proxy)||proxy==='enhanced'&&!enhancedAllowed)throw fcError('unsafe_url',400);
 const response=await firecrawlRequest(key,'scrape',{...rest,timeoutMs,body:{url,formats:markdownOnly?['markdown']:['markdown','links',...(html?['html']:[])],timeout:timeoutMs,proxy,onlyMainContent,onlyCleanContent:false,checkPromptInjection:false,redactPII:false,skipTlsVerification:false,parsers:[],maxAge:0,storeInCache:false}});
 const data=response.data;if(!data||typeof data!=='object')throw fcError('response');
 const targetStatus=Number(data.metadata?.statusCode)||null;
 const finalUrl=safeListingUrl(data.metadata?.url||data.metadata?.sourceURL||url,hosts);
 const evidence=String(data.markdown||'').slice(0,3000);
 const blocked=targetStatus===401||targetStatus===403||targetStatus===429||/captcha|verify (?:you are|you're) human|access denied|доступ (?:ограничен|запрещён)|подтвердите.{0,30}(?:человек|робот)/i.test(evidence);
 const numeric=v=>typeof v==='number'&&Number.isFinite(v)&&v>=0?v:null;
 const creditsReported=numeric(data.metadata?.creditsUsed??data.metadata?.provider?.creditsCost);
 const metrics={...response,data:undefined,finalUrl,targetStatus,creditsReported,creditsEstimate:1};
 if(blocked)throw fcError('blocked',422,metrics);
 if(targetStatus!==null&&targetStatus>=400||data.metadata?.error)throw fcError('target',422,metrics);
 if(data.metadata?.contentType&&!/html|text\/plain/i.test(data.metadata.contentType))throw fcError('target',422,metrics);
 return {...metrics,data};
}
export async function testFirecrawlConnection(key,options){const r=await firecrawlRequest(key,'team/credit-usage',{...options,retry:false});const remaining=r.data?.remainingCredits;if(typeof remaining!=='number'||!Number.isFinite(remaining))throw fcError('response');return {ok:true,remainingCredits:remaining,account:{remainingCredits:remaining,planCredits:r.data.planCredits??null,billingPeriodStart:r.data.billingPeriodStart??null,billingPeriodEnd:r.data.billingPeriodEnd??null},requests:r.requests,durationMs:r.durationMs};}
