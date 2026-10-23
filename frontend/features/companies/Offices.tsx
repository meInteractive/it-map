import type {CSSProperties} from 'react';
import type {Entity} from '../../../shared/contracts';
import metro from '../../../src/metro.json';
import mcd3 from '../../../src/mcd3.json';
const key=(v:string)=>v.trim().replaceAll('ё','е').toLowerCase().split('(')[0].trim();
const mcc='Московское центральное кольцо';
const mcd=new Map<string,{colors:string[];lines:string[]}>([
 ['гражданская',{colors:['#E94282'],lines:['МЦД-2']}],['калитники',{colors:['#E94282'],lines:['МЦД-2']}],
 ['серп и молот',{colors:['#40B778'],lines:['МЦД-4']}],['москва-сити',{colors:['#40B778'],lines:['МЦД-4']}],['нижегородская',{colors:['#40B778'],lines:['МЦД-4']}]
]);
mcd3.stations.forEach(s=>mcd.set(key(s.n),{colors:['#E56B4B'],lines:['МЦД-3']}));
export function StationChip({label}:{label:string}){
 const k=key(label),lower=label.toLowerCase(),entries=metro.stations.filter(s=>key(s.n)===k),regular=entries.filter(s=>!s.l.includes(mcc));
 const selected=lower.includes('(мцк)')&&!lower.includes('метро и мцк')?entries.filter(s=>s.l.includes(mcc)):!lower.includes('мцк')&&regular.length?regular:entries;
 const colors=selected.flatMap(s=>s.c),lines=selected.flatMap(s=>s.l),commuter=mcd.get(k);
 if(commuter&&(lower.includes('мцд')||!entries.length||k==='москва-сити')){colors.push(...commuter.colors);lines.push(...commuter.lines);}
 const palette=[...new Set(colors.filter(Boolean))];if(!palette.length)palette.push('#64748b');
 const fill=palette.length===1?palette[0]:'linear-gradient(90deg,'+palette.map((c,i)=>c+' '+i*100/palette.length+'% '+(i+1)*100/palette.length+'%').join(',')+')';
 const luminance=palette.reduce((sum,c)=>{const hex=c.replace('#','');if(hex.length!==6)return sum+.35;const [r,g,b]=[0,2,4].map(i=>parseInt(hex.slice(i,i+2),16)/255).map(v=>v<=.03928?v/12.92:((v+.055)/1.055)**2.4);return sum+.2126*r+.7152*g+.0722*b;},0)/palette.length;
 return <span className="office-metro-chip" style={{'--station-fill':fill,'--station-ink':luminance>.48?'#172033':'#fff'} as CSSProperties} title={lines.length?label+' — '+[...new Set(lines)].join(', '):label}>{label}</span>;
}
export function Offices({entity}:{entity:Entity}){return !entity.offices?.length?<span className="office-metro-missing">Адрес не указан</span>:<div className="company-office-list">{entity.offices.map((o,i)=>{const stations=String(o.metro||'').split(';').map(v=>v.trim()).filter(v=>v&&v!=='—');return <div className="company-office-item" key={i}><div className="company-office-address">{o.address}</div><div className="company-office-metro">{stations.length?stations.map((s,j)=><StationChip key={j} label={s}/>):<span className="office-metro-missing">Метро не указано</span>}</div></div>;})}</div>;}
