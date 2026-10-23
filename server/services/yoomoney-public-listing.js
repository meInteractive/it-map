import {preflightListingUrl,safeListingUrl,fcError} from '../integrations/firecrawl.js';
import {candidateHash,canonicalCandidateUrl} from '../repositories/vacancy-candidates.js';
import {decodeListingText,extractVacancyListing,LISTING_ADAPTERS} from './vacancy-listing-extractor.js';
const SITE='https://jobs.yoomoney.ru',API='https://store.tildaapi.com/api/getproductslist/';
const incomplete=message=>Object.assign(new Error(message),{code:'listing_incomplete',status:422});
const digits=value=>typeof value==='string'&&/^[1-9]\d{0,14}$/.test(value);
export const usesYooMoneyPublicListing=source=>source.sourceId==='companySite'&&source.config.adapter==='yoomoney'&&!source.config.titleClass&&!source.config.detailPathPrefix&&new URL(source.url).origin===SITE;
export function yooMoneyCatalogUrl(cursor){
 if(!digits(cursor.partuid)||!digits(cursor.recid)||!Number.isSafeInteger(cursor.size)||cursor.size<1||cursor.size>100||!Number.isSafeInteger(cursor.slice)||cursor.slice<1||cursor.slice>200||!Array.isArray(cursor.fingerprints)||cursor.fingerprints.length>200||cursor.fingerprints.some(v=>typeof v!=='string'||!/^([a-f0-9]{64})$/.test(v))||cursor.total!==null&&(!Number.isSafeInteger(cursor.total)||cursor.total<0||cursor.total>10000))throw incomplete('Некорректное продолжение открытого каталога ЮMoney.');
 const u=new URL(API);u.searchParams.set('storepartuid',cursor.partuid);u.searchParams.set('recid',cursor.recid);
 if(cursor.slice===1){u.searchParams.set('getparts','true');u.searchParams.set('getoptions','true');}else u.searchParams.set('slice',String(cursor.slice));
 u.searchParams.set('size',String(cursor.size));u.searchParams.set('flag_root','withroot');return u.href;
}
export function yooMoneyStoreFromHtml(html){
 // Read the same public options used by t_store_init; never execute site JS.
 const matches=[...html.matchAll(/var options=(\{[\s\S]{1,20000}?\});t_onFuncLoad\(['"]t_store_init['"],function\(\)\s*\{t_store_init\(['"](\d+)['"],options\)/g)];
 if(matches.length!==1)throw incomplete('На странице ЮMoney не найден единственный подтверждённый каталог вакансий.');
 const options=matches[0][1],recid=options.match(/\brecid:['"](\d+)['"]/)?.[1],partuid=options.match(/\bstorepart:['"](\d+)['"]/)?.[1],size=Number(options.match(/\bsize:(\d+)(?:,|\})/)?.[1]);
 if(recid!==matches[0][2]||/\bfilters\s*:|defaultSort:\s*\{/.test(options))throw incomplete('Конфигурация каталога ЮMoney изменилась или содержит неизвестный фильтр.');
 const cursor={phase:'catalog',recid,partuid,size,slice:1,total:null,fingerprints:[]};yooMoneyCatalogUrl(cursor);return cursor;
}
async function boundedText(response){
 const reader=response.body?.getReader();if(!reader)throw incomplete('Каталог ЮMoney вернул пустой ответ.');let size=0;const parts=[];
 while(true){const item=await reader.read();if(item.done)break;size+=item.value.byteLength;if(size>2*1024*1024){await reader.cancel();throw incomplete('Ответ каталога ЮMoney превысил допустимый размер.');}parts.push(item.value);}
 const out=new Uint8Array(size);let at=0;for(const part of parts){out.set(part,at);at+=part.byteLength;}return new TextDecoder().decode(out);
}
export async function loadYooMoneyPublicPage(url,cursor={phase:'source'},{signal,timeoutMs=45000,assertActive=async()=>{}}={}){
 const started=Date.now(),catalog=cursor.phase==='catalog',hosts=[catalog?'store.tildaapi.com':'jobs.yoomoney.ru'];
 safeListingUrl(url,hosts);
 if(catalog&&url!==yooMoneyCatalogUrl(cursor)||!catalog&&(new URL(url).origin!==SITE||!/^\/(?:vacancies\/?)?$/.test(new URL(url).pathname)||new URL(url).search||new URL(url).hash))throw incomplete('Адрес не соответствует текущей порции каталога ЮMoney.');
 await assertActive();const finalUrl=await preflightListingUrl(url,hosts,{signal});await assertActive();
 if(catalog&&finalUrl!==url||!catalog&&new URL(finalUrl).origin!==SITE)throw incomplete('Каталог ЮMoney перенаправил запрос на другой адрес.');
 let response,requests=0;
 try{
  requests=1;response=await fetch(finalUrl,{headers:{accept:catalog?'application/json':'text/html'},redirect:'manual',signal:signal?AbortSignal.any([signal,AbortSignal.timeout(timeoutMs)]):AbortSignal.timeout(timeoutMs)});
  if([401,403,429].includes(response.status))throw fcError('blocked',422,{targetStatus:response.status});
  if(!response.ok||response.status>=300)throw fcError('target',422,{targetStatus:response.status});
  const text=await boundedText(response);await assertActive();
  const diagnostics={hasMarkdown:false,hasHtml:!catalog,hasLinks:false,linkCount:0,pairCount:0,rejected:0,unpaired:0,ambiguous:0,limitReached:false,publicCatalog:true};
  let next=null,jobs=[],emptyConfirmed=false,coverage=null,nextCursor=cursor,listingUrl=cursor.listingUrl||finalUrl;
  if(!catalog){
   if(new URL(finalUrl).pathname==='/'){
    next=extractVacancyListing({html:text},{pageUrl:finalUrl,hosts,config:{adapter:'yoomoney',html:true}}).nextUrl;
    if(!next)throw incomplete('Главная страница ЮMoney не содержит явной ссылки на полный список вакансий.');
    nextCursor={phase:'store',listingUrl:next};
   }else{nextCursor={...yooMoneyStoreFromHtml(text),listingUrl:finalUrl};next=yooMoneyCatalogUrl(nextCursor);}
  }else{
   let data;try{data=JSON.parse(text);}catch{throw incomplete('Каталог ЮMoney вернул не JSON.');}
   if(!data||typeof data!=='object'||Array.isArray(data)||String(data.partuid)!==cursor.partuid||!Number.isSafeInteger(data.total)||data.total<0||data.total>10000||data.slice!==cursor.slice||!Array.isArray(data.products)||data.products.length>cursor.size||data.products.some(p=>!p||typeof p!=='object'||Array.isArray(p)))throw incomplete('Ответ каталога ЮMoney не соответствует запрошенной категории или порции.');
   if(cursor.total!==null&&data.total!==cursor.total)throw incomplete('Количество вакансий ЮMoney изменилось между порциями. Полнота не подтверждена.');
   if(data.products.length!==Math.min(cursor.size,Math.max(0,data.total-(cursor.slice-1)*cursor.size)))throw incomplete('Каталог ЮMoney вернул неполную или повторную порцию.');
   const fingerprint=await candidateHash(JSON.stringify(data.products.map(p=>[p.uid,p.url])));
   if(cursor.fingerprints.includes(fingerprint))throw incomplete('Каталог ЮMoney повторил предыдущую порцию.');
   const identities=new Set();
   for(const product of data.products){
    try{
     const uid=typeof product.uid==='number'&&Number.isSafeInteger(product.uid)?String(product.uid):product.uid,title=decodeListingText(product.title),detail=safeListingUrl(product.url,['jobs.yoomoney.ru']),u=new URL(detail);
     if(typeof product.title!=='string'||!digits(uid)||!title||Array.from(title).length>500||!/[a-zа-яё]/i.test(title)||!LISTING_ADAPTERS.yoomoney.matches(u)||u.search||u.hash||product.buttonlink&&canonicalCandidateUrl(safeListingUrl(product.buttonlink,['jobs.yoomoney.ru']))!==canonicalCandidateUrl(detail)||identities.has(uid))throw incomplete('Неподтверждённая карточка ЮMoney.');
     identities.add(uid);jobs.push({title,url:detail,externalJobId:'tilda:'+uid});
    }catch{diagnostics.unpaired++;}
   }
   const hasNext=data.nextslice!==undefined&&data.nextslice!==null&&data.nextslice!==0&&data.nextslice!=='';
   if(hasNext&&(!Number.isSafeInteger(data.nextslice)||data.nextslice!==cursor.slice+1||cursor.slice*cursor.size>=data.total)||!hasNext&&cursor.slice*cursor.size<data.total)throw incomplete('Каталог ЮMoney не подтвердил непрерывное продолжение списка.');
   nextCursor={...cursor,total:data.total,slice:hasNext?data.nextslice:cursor.slice,phase:hasNext?'catalog':'complete',fingerprints:[...cursor.fingerprints,fingerprint]};
   if(hasNext)next=yooMoneyCatalogUrl(nextCursor);
   emptyConfirmed=data.total===0&&!hasNext;coverage={total:data.total,slice:cursor.slice,received:data.products.length,complete:!hasNext};diagnostics.pairCount=data.products.length;diagnostics.linkCount=jobs.length;diagnostics.hasLinks=jobs.length>0;
  }
  return {requests,finalUrl,listingUrl,providerStatus:response.status,targetStatus:response.status,durationMs:Date.now()-started,profile:'public',creditsReported:0,creditsEstimate:0,cursor:nextCursor,coverage,extraction:{ok:true,jobs,nextUrl:next,emptyConfirmed,diagnostics}};
 }catch(error){
  const e=error.code?error:fcError(error.name==='TimeoutError'||error.name==='AbortError'?'timeout':'network');throw Object.assign(e,{requests,creditsReported:0,creditsEstimate:0});
 }
}
