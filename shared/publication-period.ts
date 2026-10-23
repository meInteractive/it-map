export type PublicationPeriod='week'|'month'|'twoMonths'|'archive'|'all';
export const publicationPeriods:Record<PublicationPeriod,string>={week:'Неделя',month:'Месяц',twoMonths:'2 месяца',archive:'Архив',all:'Все'};
export function publicationDate(value:string|null|undefined){
 const text=String(value||'').trim().slice(0,10);if(!/^\d{4}-\d{2}-\d{2}$/.test(text))return null;
 const [year,month,day]=text.split('-').map(Number),date=new Date(Date.UTC(year,month-1,day));
 return date.getUTCFullYear()===year&&date.getUTCMonth()===month-1&&date.getUTCDate()===day?{year,month,day,time:date.getTime()}:null;
}
export function moscowPublicationDay(now:number|Date=Date.now()){
 const parts=new Intl.DateTimeFormat('en-US',{timeZone:'Europe/Moscow',year:'numeric',month:'2-digit',day:'2-digit'}).formatToParts(now),value=(key:string)=>parts.find(p=>p.type===key)!.value;
 return publicationDate(value('year')+'-'+value('month')+'-'+value('day'))!;
}
export function publicationBounds(period:PublicationPeriod,now:number|Date=Date.now()){
 const today=moscowPublicationDay(now);
 const monthsAgo=(months:number)=>{const first=new Date(Date.UTC(today.year,today.month-1-months,1)),last=new Date(Date.UTC(first.getUTCFullYear(),first.getUTCMonth()+1,0)).getUTCDate();return Date.UTC(first.getUTCFullYear(),first.getUTCMonth(),Math.min(today.day,last));};
 return {from:period==='week'?today.time-6*86400000:monthsAgo(period==='month'?1:2),to:today.time,archive:period==='archive',all:period==='all'};
}
export function effectivePublicationDate(v:{publishedAt?:string|null;loadedAt?:string|null;createdAt?:string|null}){
 return publicationDate(v.publishedAt)||publicationDate(v.loadedAt)||publicationDate(v.createdAt);
}
export function matchesPublicationBounds(v:Parameters<typeof effectivePublicationDate>[0],bounds:ReturnType<typeof publicationBounds>){
 if(bounds.all)return true;
 const date=effectivePublicationDate(v);return !!date&&(bounds.archive?date.time<bounds.from:date.time>=bounds.from&&date.time<=bounds.to);
}
export function matchesPublicationPeriod(v:Parameters<typeof effectivePublicationDate>[0],period:PublicationPeriod,now:number|Date=Date.now()){return matchesPublicationBounds(v,publicationBounds(period,now));}

