// Provider INPUT/logs are untrusted: expose only fixed enums and bounded IDs.
// Never return raw URLs, query values, arbitrary field names or log excerpts.
import {hhTitleFilterEvidence} from './hh-title-filter.js';
const integer=(value,max)=>typeof value==='string'&&/^\d{1,6}$/.test(value)&&Number(value)<=max?Number(value):null;
const boolean=value=>typeof value==='boolean'?value:null;
const bounded=(value,max)=>Number.isSafeInteger(value)&&value>=1&&value<=max?value:null;
export function hhUrlEvidence(value){
 if(typeof value!=='string'||value.length>4000)return null;
 let u;try{u=new URL(value);}catch{return null;}
 if(u.protocol!=='https:'||!/(^|\.)hh\.ru$/.test(u.hostname)||u.username||u.password||u.port)return null;
 const vacancy=u.pathname.match(/^\/vacancy\/([1-9]\d{0,11})\/?$/),employer=u.pathname.match(/^\/employer\/([1-9]\d{0,11})\/?$/);
 if(!vacancy&&!employer&&u.pathname!=='/search/vacancy')return null;
 const id=employer?.[1]||u.searchParams.get('employer_id'),sort=u.searchParams.get('order_by');
 return {kind:vacancy?'vacancy':employer?'employer':'search',vacancyId:vacancy?.[1]||null,employerId:/^[1-9]\d{0,11}$/.test(id||'')?id:null,
  page:u.searchParams.has('page')?integer(u.searchParams.get('page'),100):0,
  perPage:u.searchParams.has('per_page')?integer(u.searchParams.get('per_page'),100):null,
  orderBy:sort===null?'default':['publication_time','relevance','salary_desc','salary_asc'].includes(sort)?sort:'other',
  titleFilter:hhTitleFilterEvidence(u.searchParams.get('text')),
  nameOnlyQuery:/^NAME:\(/.test(u.searchParams.get('text')||''),
  hasDateFilter:['date_from','date_to','search_period'].some(key=>u.searchParams.has(key))};
}
export function apifyInputEvidence(input,{operationKey,expectedVacancyUrl}={}){
 const source=hhUrlEvidence(input?.startUrl),page=operationKey?.match(/^listing:[0-9a-f-]{36}:(\d{1,2})$/)?.[1],expected=hhUrlEvidence(expectedVacancyUrl);
 return {source,pageMatchesOperation:page!==undefined&&source?source.kind==='search'&&source.page===Number(page):null,
  vacancyMatchesCandidate:expected?.vacancyId&&source?source.kind==='vacancy'&&source.vacancyId===expected.vacancyId:null,
  apiOnly:boolean(input?.api_only),resultsWanted:bounded(input?.results_wanted,10000),
  maxPages:bounded(input?.max_pages,100),includeRawData:boolean(input?.include_raw_data),
  proxyEnabled:boolean(input?.proxyConfiguration?.useApifyProxy),usesResidential:Array.isArray(input?.proxyConfiguration?.apifyProxyGroups)&&input.proxyConfiguration.apifyProxyGroups.includes('RESIDENTIAL')};
}
export function apifyLogEvidence(log){
 const hhRequests=[];
 for(const match of log.matchAll(/https:\/\/(?:[a-z0-9-]+\.)*hh\.ru\/(?:search\/vacancy|vacancy\/\d+|employer\/\d+)(?:\?[^\s"'<>]*)?/gi)){
  const source=hhUrlEvidence(match[0]);if(source&&!hhRequests.some(x=>JSON.stringify(x)===JSON.stringify(source)))hhRequests.push(source);if(hhRequests.length===10)break;
 }
 return {hhRequests,eventCounts:{fallback:(log.match(/\bfallback\b/gi)||[]).length,enrichment:(log.match(/\b(?:enrich|enriched|enrichment)\b/gi)||[]).length,missingDescription:(log.match(/(?:missing|empty|no)\s+(?:job\s+)?description/gi)||[]).length}};
}
