import {safeListingUrl} from '../integrations/firecrawl.js';
import {canonicalCandidateUrl} from '../repositories/vacancy-candidates.js';
import {rabotaNextPage} from './rabota-listing.js';
const navigation=/^(?:главная|о компании|вакансии(?: \d+)?|все вакансии|работа в .+|вакансии компании.+|контакты|подробнее|откликнуться|отправить резюме|наши вакансии|хочу в команду|смотреть вакансии|назад|далее|следующая|next|previous|apply|learn more|read more|jobs|careers|vacancies|open positions|job openings|home|about|contact)$/i;
export function decodeListingText(value){return String(value||'').replace(/<[^>]*>/g,' ').replace(/&#(x[0-9a-f]+|\d+);/gi,(_,n)=>{const c=n[0].toLowerCase()==='x'?parseInt(n.slice(1),16):Number(n);return c>0&&c<=0x10ffff&&! (c>=0xd800&&c<=0xdfff)?String.fromCodePoint(c):'';}).replace(/&(amp|quot|apos|lt|gt|nbsp);/g,(_,n)=>({amp:'&',quot:'"',apos:"'",lt:'<',gt:'>',nbsp:' '}[n])).replace(/\\([()[\]_*])/g,'$1').replace(/\s+/g,' ').trim();}
// Registry extends extraction, never transport or main-vacancy writes.
export const LISTING_ADAPTERS={
 yoomoney:{matches:u=>u.hostname==='jobs.yoomoney.ru'&&/^\/[a-z0-9]+(?:-[a-z0-9]+)*\/?$/.test(u.pathname)&&!/^\/(?:vacancies|privacy|contacts|about)\/?$/.test(u.pathname),externalId:()=>null},
 selectel:{matches:u=>/^\/careers\/all\/vacancy\/\d+\/?$/.test(u.pathname),externalId:u=>u.pathname.match(/\d+/)?.[0]||null,markdownTitle:raw=>raw.match(/\*\*([\s\S]+?)\*\*/)?.[1]||''},
 generic:{matches:u=>/\/(?:vacancy|vacancies|jobs?|positions?|openings?)\/[^/]+\/?$/i.test(u.pathname)&&! /\/(?:all|list|search|index|categories|teams|locations)\/?$/i.test(u.pathname),externalId:()=>null},
 habr:{matches:u=>/^\/vacancies\/\d+\/?$/.test(u.pathname),externalId:u=>u.pathname.match(/\d+/)?.[0]||null},
 getmatch:{matches:u=>/^\/vacancies\/[A-Za-z0-9_-]+\/?$/.test(u.pathname),externalId:u=>u.pathname.split('/')[2]},
 rabota:{matches:u=>/^\/vacancy\/\d+\/?$/.test(u.pathname),externalId:u=>u.pathname.match(/\d+/)?.[0]||null,normalizeUrl:u=>{if(['rabota.ru','www.rabota.ru'].includes(u.hostname))u.hostname='www.rabota.ru';u.searchParams.delete('search_id');}},
};
export function defaultListingAdapter(sourceUrl){const host=new URL(sourceUrl).hostname;return host==='jobs.yoomoney.ru'?'yoomoney':host==='selectel.ru'?'selectel':host==='career.habr.com'?'habr':/(^|\.)getmatch.ru$/.test(host)?'getmatch':/(^|\.)rabota.ru$/.test(host)?'rabota':'generic';}
function htmlPairs(html,titleClass){
 const pairs=[];const re=/<a\b([^>]{0,4096})>([\s\S]*?)<\/a\s*>/gi;let match;
 while((match=re.exec(html))&&pairs.length<10000){const href=match[1].match(/\bhref\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))/i);if(!href)continue;
  let inner=match[2],title='';
  if(titleClass){const elements=/<([a-z][a-z0-9]*)\b([^>]{0,4096})>/gi;let e;while((e=elements.exec(inner))){const classes=e[2].match(/\bclass\s*=\s*["']([^"']*)["']/i)?.[1]?.split(/\s+/)||[];if(classes.includes(titleClass)){const tail=inner.slice(elements.lastIndex);const end=tail.search(new RegExp('</'+e[1]+'\\s*>','i'));if(end>=0)title=tail.slice(0,end);break;}}if(!title){if((match[1].match(/\bclass\s*=\s*["']([^"']*)["']/i)?.[1]?.split(/\s+/)||[]).includes(titleClass))title=inner;else continue;}}
  else title=inner.match(/<h[1-6]\b[^>]*>([\s\S]*?)<\/h[1-6]>/i)?.[1]||inner;
  pairs.push({title:decodeListingText(title),url:decodeListingText(href[1]??href[2]??href[3]),kind:'html'});
 }
 return pairs;
}
function markdownPairs(markdown){const rows=[];const re=/(?<!!)\[([^\]]{1,2000})\]\(<?([^\s<>]*?(?:\([^\s<>]*\)[^\s<>]*)?)>?(?:\s+"[^"\n]*")?\)/g;let m;while((m=re.exec(markdown))&&rows.length<10000)rows.push({title:decodeListingText(m[1].match(/\*\*([\s\S]+?)\*\*/)?.[1]||m[1].replace(/[*_`]/g,'')),rawTitle:m[1],url:m[2],kind:'markdown'});return rows;}
// Rabota renders a heading and an empty mobile overlay link as siblings.
// Couple them only within one explicit card, never by global title/link order.
function rabotaCardPairs(html){
 const starts=[...html.matchAll(/<(?:div|article|li)\b[^>]{0,4096}\bclass\s*=\s*["'][^"']*\bvacancy-preview-card\b[^"']*["'][^>]*>/gi)].filter(m=>{const classes=m[0].match(/\bclass\s*=\s*["']([^"']*)["']/i)?.[1]?.split(/\s+/)||[];return classes.includes('vacancy-preview-card');});
 const pairs=[];
 for(let i=0;i<Math.min(starts.length,200);i++){
  const start=starts[i],tag=start[0].match(/^<([a-z]+)/i)[1],tags=new RegExp('<\\/?'+tag+'\\b[^>]*>','gi');tags.lastIndex=start.index+start[0].length;let depth=1,end=0,t;while((t=tags.exec(html))){depth+=t[0].startsWith('</')?-1:1;if(depth===0){end=tags.lastIndex;break;}}if(!end)continue;
  const card=html.slice(start.index,end),title=card.match(/<h[1-6]\b[^>]*>([\s\S]*?)<\/h[1-6]>/i)?.[1];if(!title)continue;
  const links=[...card.matchAll(/<a\b[^>]{0,4096}\bhref\s*=\s*["']([^"']+)["'][^>]*>/gi)].map(m=>decodeListingText(m[1])).filter(u=>/\/vacancy\/\d+\/?(?:[?#]|$)/.test(u));
  const ids=new Set(links.map(u=>u.match(/\/vacancy\/(\d+)/)[1]));if(ids.size!==1)continue;
  for(const url of links)pairs.push({title:decodeListingText(title),url,kind:'rabota-card'});
 }
 return pairs;
}
export function extractVacancyListing(data,{pageUrl,hosts,config={}}){
 const markdown=typeof data.markdown==='string'?data.markdown:'',html=config.html&&typeof data.html==='string'?data.html:'';
 const adapter=LISTING_ADAPTERS[config.adapter||defaultListingAdapter(pageUrl)]||LISTING_ADAPTERS.generic;
 const yoomoney=adapter===LISTING_ADAPTERS.yoomoney;
 // Tilda card names are paired only with their enclosing card link. Root slug
 // URLs alone cannot distinguish a vacancy from navigation or a salary link.
 const pairs=yoomoney?htmlPairs(html,config.titleClass||'js-store-prod-name'):[...(html&&adapter===LISTING_ADAPTERS.rabota&&!config.titleClass?rabotaCardPairs(html):[]),...(html?htmlPairs(html,config.titleClass):[]),...(!config.titleClass?markdownPairs(markdown):[])];
 const jobs=new Map(),conflicts=new Set(),unresolvedRabota=new Set();let rejected=0,unpaired=0,ambiguous=0;const named=new Set();
 for(const pair of pairs){
  let url;try{url=new URL(safeListingUrl(new URL(pair.url,pageUrl).href,hosts));}catch{rejected++;continue;}
  adapter.normalizeUrl?.(url);
  const known=config.detailPathPrefix?url.pathname.startsWith(config.detailPathPrefix)&&url.pathname.length>config.detailPathPrefix.length:adapter.matches(url);
  if(!known||canonicalCandidateUrl(url.href)===canonicalCandidateUrl(pageUrl)){rejected++;continue;}
  if(adapter.markdownTitle&&pair.kind==='markdown'){pair.title=decodeListingText(adapter.markdownTitle(pair.rawTitle));if(!pair.title){rejected++;continue;}}
  if(navigation.test(pair.title)){rejected++;continue;}
  if(!pair.title||Array.from(pair.title).length>500||!/[a-zа-яё]/i.test(pair.title)||/\u0000/.test(pair.title)){if(adapter===LISTING_ADAPTERS.rabota)unresolvedRabota.add(canonicalCandidateUrl(url.href));else unpaired++;continue;}
  const canonical=canonicalCandidateUrl(url.href);named.add(canonical);if(conflicts.has(canonical))continue;
  const previous=jobs.get(canonical);if(previous&&previous.title!==pair.title){
   // A single-ID card heading outranks salary/overlay links for the same ID.
   if(previous.kind==='rabota-card'&&pair.kind!=='rabota-card')continue;
   // HTML heading/class is more precise than a Markdown card containing metadata.
   if(previous.kind==='html'&&pair.kind==='markdown')continue;
   if(previous.kind==='markdown'&&pair.kind==='html'){jobs.set(canonical,{...pair,url:url.href,externalJobId:adapter.externalId(url)});continue;}
   ambiguous++;conflicts.add(canonical);jobs.delete(canonical);continue;
  }
  if(jobs.size<200&&!previous)jobs.set(canonical,{...pair,url:url.href,externalJobId:adapter.externalId(url)});
 }
 const links=Array.isArray(data.links)?data.links.filter(v=>typeof v==='string').slice(0,10000):[];
 for(const link of yoomoney?[]:links){try{const u=new URL(safeListingUrl(new URL(link,pageUrl).href,hosts));adapter.normalizeUrl?.(u);if(adapter.matches(u)&&!named.has(canonicalCandidateUrl(u.href))){if(adapter===LISTING_ADAPTERS.rabota)unresolvedRabota.add(canonicalCandidateUrl(u.href));else unpaired++;}}catch{/* counted in supplied links */}}
 if(adapter===LISTING_ADAPTERS.rabota)unpaired=[...unresolvedRabota].filter(url=>!named.has(url)).length;
 // An explicit empty-state message is evidence; an arbitrary successful HTML
 // page with no matching links is an extraction failure, never an empty success.
 const unavailable=adapter===LISTING_ADAPTERS.getmatch&&/не смогли загрузить данные/i.test(decodeListingText(markdown+' '+html));
 const emptyConfirmed=!unavailable&&/\bno (?:open |current |available )?(?:jobs|vacancies|positions)(?: available| found)?\b|(?:нет|не найдено) (?:открытых |актуальных |доступных )?вакансий|вакансий (?:пока )?нет/i.test(decodeListingText(markdown+' '+html));
 let nextUrl=null;
 const pagination=config.pagination||(adapter===LISTING_ADAPTERS.rabota?{queryParam:'page'}:null);
 if(pagination){for(const pair of pairs){const current=new URL(pageUrl),key=pagination.queryParam,numericNext=adapter===LISTING_ADAPTERS.rabota&&/^\d+$/.test(pair.title)&&Number(pair.title)===Number(current.searchParams.get(key)||1)+1;if(!numericNext&&!/^(?:далее|следующая(?: страница)?|next(?: page)?|→|›|»|след\.)$/i.test(pair.title))continue;try{const next=new URL(safeListingUrl(new URL(pair.url,pageUrl).href,hosts));if(next.origin===current.origin&&next.pathname===current.pathname&&next.searchParams.has(key)){const a=new URL(next),b=new URL(current);a.searchParams.delete(key);b.searchParams.delete(key);a.searchParams.sort();b.searchParams.sort();if(a.href===b.href&&next.href!==current.href){nextUrl=next.href;break;}}}catch{/* never follow an unapproved next link */}}}
 // The homepage is a preview. Follow only the explicit unfiltered same-origin
 // vacancy-list link, within the normal page/request limits. Never infer URLs.
 if(yoomoney&&new URL(pageUrl).pathname==='/'){for(const pair of [...markdownPairs(markdown),...htmlPairs(html)]){if(!/^(?:хочу в команду|вакансии|все вакансии)$/i.test(pair.title))continue;try{const next=new URL(safeListingUrl(new URL(pair.url,pageUrl).href,hosts));if(next.origin===new URL(pageUrl).origin&&/^\/vacancies\/?$/.test(next.pathname)&&!next.search&&!next.hash){nextUrl=next.href;break;}}catch{/* same allowed-host guard as detail URLs */}}}
 const incomplete=yoomoney&&!nextUrl&&(/загрузить ещ[её]|load more/i.test(decodeListingText(markdown+' '+html))||new URL(pageUrl).pathname==='/')?'ЮMoney показывает только часть списка; доступного продолжения нет. Полнота не подтверждена.':null;
 if(!nextUrl&&adapter===LISTING_ADAPTERS.rabota)nextUrl=rabotaNextPage(html,pageUrl);
 return {errorCode:unavailable?'listing_unavailable':null,jobs:[...(unavailable?[]:jobs.values())].map(({kind,rawTitle,...job})=>job),nextUrl,emptyConfirmed,ok:!unavailable&&(jobs.size>0||emptyConfirmed),diagnostics:{incomplete,hasMarkdown:!!markdown,hasHtml:!!html,hasLinks:links.length>0,linkCount:links.length,pairCount:pairs.length,rejected,unpaired,ambiguous,limitReached:jobs.size>=200},adapter:config.adapter||defaultListingAdapter(pageUrl)};
}
