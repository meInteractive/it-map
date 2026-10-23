// Public structured job data only: fixed HH URL, no credentials or redirects.
const MAX_BYTES=2000000;
export function hhHtmlText(html){
 return html.replace(/<!--[\s\S]*?-->/g,'').replace(/<(script|style)\b[^>]*>[\s\S]*?<\/\1\s*>/gi,'').replace(/<\/(?:p|div|h[1-6]|ul|ol)>|<br\s*\/?\s*>/gi,'\n').replace(/<li\b[^>]*>/gi,'\n- ').replace(/<[^>]*>/g,'').replace(/&(#x[0-9a-f]+|#\d+|amp|lt|gt|quot|apos|nbsp);/gi,(m,e)=>{if(e[0]==='#'){const n=e[1].toLowerCase()==='x'?parseInt(e.slice(2),16):Number(e.slice(1));return n>0&&n<=0x10ffff&&!(n>=0xd800&&n<=0xdfff)?String.fromCodePoint(n):'';}return {amp:'&',lt:'<',gt:'>',quot:'"',apos:"'",nbsp:' '}[e.toLowerCase()];}).replace(/\n[ \t]+/g,'\n').replace(/\n{3,}/g,'\n\n').trim();
}
export function parsePublicHhJob(html,id,{now=Date.now()}={}){
 if(typeof html!=='string'||!/^[1-9]\d{0,11}$/.test(String(id)))return null;
 const jobs=[];
 const visit=value=>{if(!value||typeof value!=='object')return;if(Array.isArray(value)){value.forEach(visit);return;}if(value['@type']==='JobPosting')jobs.push(value);if(Array.isArray(value['@graph']))value['@graph'].forEach(visit);};
 for(const match of html.matchAll(/<script\b[^>]*\btype\s*=\s*["']application\/ld\+json["'][^>]*>([\s\S]*?)<\/script\s*>/gi)){try{visit(JSON.parse(match[1]));}catch{}}
 const matching=jobs.filter(j=>String(j.identifier?.value)===String(id));if(matching.length!==1)return null;
 const j=matching[0];if(typeof j.title!=='string'||typeof j.description!=='string'||j.validThrough&&(!Number.isFinite(Date.parse(j.validThrough))||Date.parse(j.validThrough)<now))return null;
 if(j.url){try{const u=new URL(j.url);if(u.protocol!=='https:'||!/(^|\.)hh\.ru$/.test(u.hostname)||u.pathname!==`/vacancy/${id}`)return null;}catch{return null;}}
 const date=typeof j.datePosted==='string'&&/^\d{4}-\d\d-\d\d(?:T|$)/.test(j.datePosted)&&Number.isFinite(Date.parse(j.datePosted))?j.datePosted.slice(0,10):null;
 return {vacancy_id:String(id),url:`https://hh.ru/vacancy/${id}`,title:hhHtmlText(j.title),description_text:hhHtmlText(j.description),company:typeof j.hiringOrganization?.name==='string'?hhHtmlText(j.hiringOrganization.name):null,date_posted:date};
}
export async function fetchPublicHhJob(value,{signal}={}){
 let u;try{u=new URL(value);}catch{return null;}
 const id=u.pathname.match(/^\/vacancy\/([1-9]\d{0,11})\/?$/)?.[1];if(!id||u.protocol!=='https:'||!/(^|\.)hh\.ru$/.test(u.hostname)||u.username||u.password||u.port)return null;
 try{
  const response=await fetch(`https://hh.ru/vacancy/${id}`,{method:'GET',redirect:'manual',headers:{Accept:'text/html','User-Agent':'it-map-vacancy-reader/1.0'},signal:signal?AbortSignal.any([signal,AbortSignal.timeout(8000)]):AbortSignal.timeout(8000)});
  if(response.status!==200||!response.headers.get('content-type')?.includes('text/html')||Number(response.headers.get('content-length'))>MAX_BYTES){await response.body?.cancel();return null;}
  const reader=response.body?.getReader();if(!reader)return null;const decoder=new TextDecoder();let html='',bytes=0;
  try{for(;;){const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>MAX_BYTES){await reader.cancel();return null;}html+=decoder.decode(value,{stream:true});}html+=decoder.decode();}finally{reader.releaseLock();}
  return parsePublicHhJob(html,id);
 }catch(e){if(signal?.aborted)throw e;return null;}
}
