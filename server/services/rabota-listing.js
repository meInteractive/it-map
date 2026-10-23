// A company overview contains only featured jobs; its vacancies tab is the list.
export function rabotaListingUrl(value){
 const u=new URL(value);
 if(/(^|\.)rabota\.ru$/.test(u.hostname)&&/^\/company\/[^/]+\/?$/.test(u.pathname))u.pathname=u.pathname.replace(/\/?$/,'/vacancies/');
 return u.href;
}
export function rabotaListingHosts(host){return [...new Set([host,'www.rabota.ru','rabota.ru'])];}
export function rabotaListingCoverage(markdown,{nextUrl}={}){
 const text=String(markdown||'').replace(/\s+/g,' '),range=text.match(/Вакансии с\s*([\d ]+)\s+по\s*(\d+)/i),total=text.match(/Найдено\s+([\d ]+)\s+(?:свежих\s+)?вакансий/i);
 const last=range?Number(range[2]):null,count=total?Number(total[1].replace(/ /g,'')):null;
 return {first:range?Number(range[1].replace(/ /g,'')):null,last,total:count,incomplete:!nextUrl&&last!==null&&count!==null&&last<count};
}

// Rabota's rendered pager links to /?page=N even on a company page. Keep the
// verified company's path and filters; take only the next integer from its pager.
export function rabotaNextPage(html,pageUrl){
 const current=new URL(pageUrl),page=Number(current.searchParams.get('page')||1);
 if(!['www.rabota.ru','rabota.ru'].includes(current.hostname)||!/^\/company\/[^/]+\/vacancies\/$/.test(current.pathname)||!Number.isSafeInteger(page)||page<1)return null;
 const pager=String(html||'').match(/<div\b[^>]*class=["']pagination["'][^>]*>([\s\S]*?)<\/ul>/i)?.[1];
 if(!pager)return null;
 for(const m of pager.matchAll(/<a\b[^>]*href=["']([^"']+)["'][^>]*>([\s\S]*?)<\/a>/gi)){
  try{const href=new URL(m[1].replace(/&amp;/g,'&'),current),label=m[2].replace(/<[^>]*>/g,'').trim();
   if(href.origin!==current.origin||!['/',current.pathname].includes(href.pathname)||href.searchParams.size!==1||Number(href.searchParams.get('page'))!==page+1||label!==String(page+1))continue;
   const next=new URL(current);next.searchParams.set('page',String(page+1));return next.href;
  }catch{/* An untrusted pager cannot change origin or company. */}
 }
 return null;
}
