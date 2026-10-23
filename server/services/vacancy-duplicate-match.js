// Deterministic cross-publisher identity. Titles and employer boilerplate alone
// never identify a vacancy; distinct publications at one publisher stay distinct.
export function vacancyPublicationIdentity(value){
 try{
  const u=new URL(value);if(u.protocol!=='https:'||u.username||u.password||u.port)return null;
  const host=u.hostname.toLowerCase();let m;
  if(/(^|\.)hh\.ru$/.test(host)&&(m=u.pathname.match(/^\/vacancy\/(\d+)\/?$/)))return 'hh:'+m[1];
  if(host==='career.habr.com'&&(m=u.pathname.match(/^\/vacancies\/(\d+)\/?$/)))return 'habr:'+m[1];
  if(/^(?:www\.)?rabota\.ru$/.test(host)&&(m=u.pathname.match(/^\/vacancy\/(\d+)\/?$/)))return 'rabota:'+m[1];
  if(/^(?:www\.)?getmatch\.ru$/.test(host)&&(m=u.pathname.match(/^\/vacancies\/(\d+)(?:-[^/]*)?\/?$/)))return 'getmatch:'+m[1];
 }catch{}
 return null;
}
const plain=value=>String(value||'').normalize('NFKC').replace(/\[([^\]]+)\]\(https?:\/\/[^\s)]+\)/g,'$1').replace(/https?:\/\/[^\s<>]+/g,' ').replace(/<[^>]*>/g,' ').replace(/[*_`#>]+/g,' ').toLocaleLowerCase('ru').replaceAll('ё','е');
const tokens=value=>plain(value).replace(/team\s*lead/g,'team lead').replace(/кросс[ -]?функциональ/g,'кроссфункциональ').replace(/c\+\+/g,'cplusplus').replace(/c#/g,'csharp').match(/[\p{L}\p{N}]+/gu)||[];
const titleTokens=value=>tokens(String(value||'').replace(/([a-z])([A-Z])/g,'$1 $2'));
const normalized=value=>tokens(value).join(' ');
const tasks=/^(?:основные задачи|задачи|обязанности|чем предстоит заниматься|что предстоит делать|чем заниматься|вам предстоит|тебе предстоит|что нужно делать|предстоит заниматься)\s*[:?]?$/i;
const requirements=/^(?:основные требования|требования|требования к кандидату|требования к соискателю|мы ожидаем|мы ждем|что мы ждем|что мы ожидаем|что мы ждем от вас|что мы ожидаем от вас|мы ожидаем от вас|мы ждем от вас|для нас важно|наши ожидания|ожидания|необходимые навыки|что нужно знать|наш кандидат)\s*[:?]?$/i;
const otherSection=/^(?:условия|мы предлагаем|что мы предлагаем|будет плюсом|будет преимуществом|о компании|о команде|о вакансии|почему мы|что предлагаем|преимущества|бонусы)\s*[:?]?$/i;
export function vacancyComparisonText(value,title=''){
 const result={body:[],tasks:[],requirements:[]};let section=null;
 for(const raw of String(value||'').split(/\r?\n/)){
  const line=plain(raw).trim();if(!line)continue;
  if(tasks.test(line)){section='tasks';continue;}
  if(requirements.test(line)){section='requirements';continue;}
  if(otherSection.test(line)){section=null;continue;}
  // Source-specific wrappers are neither duties nor requirements.
  if(/^(?:компания|дата публикации|опубликовано|зарплата|место работы|локация|формат|график)\s*:/.test(line)||/^\d{1,2}\s+(?:января|февраля|марта|апреля|мая|июня|июля|августа|сентября|октября|ноября|декабря)\b/.test(line)||normalized(line)===normalized(title))continue;
  result.body.push(line);if(section)result[section].push(line);
 }
 return Object.fromEntries(Object.entries(result).map(([key,lines])=>[key,normalized(lines.join('\n'))]));
}
const intersection=(a,b)=>[...a].reduce((n,v)=>n+(b.has(v)?1:0),0);
function closeText(a,b,{minimumWords=35,minimumDistinct=22,dice=.88,containment=.90,shingles=.72}={}){
 const aw=a.split(' ').filter(Boolean),bw=b.split(' ').filter(Boolean),as=new Set(aw),bs=new Set(bw);
 if(Math.min(aw.length,bw.length)<minimumWords||Math.min(as.size,bs.size)<minimumDistinct)return false;
 const common=intersection(as,bs);if(2*common/(as.size+bs.size)<dice||common/Math.min(as.size,bs.size)<containment)return false;
 const grams=words=>new Set(words.slice(0,-2).map((_,i)=>words.slice(i,i+3).join(' '))),ag=grams(aw),bg=grams(bw);
 return intersection(ag,bg)/Math.min(ag.size,bg.size)>=shingles;
}
export function sameVacancyPublication(main,candidate,data,companyName,entityType='company'){
 if(main.company_name!==companyName||main.entity_type!==entityType)return false;
 let a,b;try{a=new URL(main.url);b=new URL(candidate.vacancyUrl);}catch{return false;}
 // Different IDs at HH/Habr/etc may be genuinely different teams or openings.
 const aid=vacancyPublicationIdentity(a.href),bid=vacancyPublicationIdentity(b.href);
 if(aid&&bid&&aid.split(':')[0]===bid.split(':')[0]&&aid!==bid)return false;
 if(a.hostname.replace(/^www\./,'')===b.hostname.replace(/^www\./,''))return false;
 const at=new Set(titleTokens(main.title)),bt=new Set(titleTokens(data.title));
 if(Math.min(at.size,bt.size)<2||intersection(at,bt)/Math.min(at.size,bt.size)<.8)return false;
 const levels=set=>new Set([...set].filter(t=>['junior','middle','senior','младший','старший'].includes(t)));
 const al=levels(at),bl=levels(bt);if(al.size&&bl.size&&!intersection(al,bl))return false;
 const x=vacancyComparisonText(main.description_md,main.title),y=vacancyComparisonText(data.description_md,data.title);
 if(!x.body||!y.body||/truncated for model/i.test(main.description_md+data.description_md))return false;
 // Compare substantive sections first, so a long shared company introduction
 // cannot hide different duties. Require enough diverse text, not repetition.
 const shared=['tasks','requirements'].filter(k=>x[k]&&y[k]);
 if(shared.length){
  if(shared.some(k=>!closeText(x[k],y[k],k==='requirements'?{minimumWords:12,minimumDistinct:10}:{})))return false;
  // A section missing from one rendition must still be present in its body.
  for(const k of ['tasks','requirements'])if(!!x[k]!==!!y[k]&&!closeText(x[k]||y[k],x[k]?y.body:x.body,{minimumWords:k==='requirements'?12:35,minimumDistinct:k==='requirements'?10:22,dice:0,containment:.95,shingles:.85}))return false;
  return true;
 }
 return closeText(x.body,y.body,{minimumWords:100,minimumDistinct:65,dice:.94,containment:.96,shingles:.88});
}
