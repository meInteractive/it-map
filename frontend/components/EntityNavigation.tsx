import {useEffect,useRef,useState} from 'react';
import type {EntityType} from '../../shared/contracts';
import {Icon} from './Common';

const items:[EntityType,string][]=[['company','Компании'],['agency','Агентства'],['channel','Каналы']];
export function EntityNavigation({section,label,icon,active,type,onSelect}:{
 section:'list'|'vacancies';label:string;icon:string;active:boolean;type:EntityType;onSelect:(type:EntityType)=>boolean;
}){
 const [open,setOpen]=useState(false),container=useRef<HTMLDivElement>(null),trigger=useRef<HTMLButtonElement>(null),focusIndex=useRef<number|null>(null),pointerType=useRef('');
 useEffect(()=>{
  if(!open)return;
  const outside=(event:PointerEvent)=>{if(!container.current?.contains(event.target as Node))setOpen(false);};
  document.addEventListener('pointerdown',outside);
  if(focusIndex.current!==null){container.current?.querySelectorAll<HTMLButtonElement>('.settings-nav-item')[focusIndex.current]?.focus();focusIndex.current=null;}
  return()=>document.removeEventListener('pointerdown',outside);
 },[open]);
 return <div className="settings-nav entity-nav" data-section-menu={section} ref={container}
  onPointerEnter={e=>{if(e.pointerType==='mouse')setOpen(true);}}
  onPointerLeave={e=>{if(e.pointerType==='mouse')setOpen(false);}}
  onBlur={e=>{if(!e.currentTarget.contains(e.relatedTarget as Node|null))setOpen(false);}}
  onKeyDown={e=>{if(e.key==='Escape'&&open){e.preventDefault();setOpen(false);trigger.current?.focus();}}}>
  <button ref={trigger} type="button" className="app-nav-button settings-nav-trigger" aria-current={active?'page':undefined} aria-expanded={open} aria-controls={section+'-navigation'}
   onPointerDown={e=>{pointerType.current=e.pointerType;}}
   onClick={e=>{if(!onSelect('company'))return;if(e?.detail&&pointerType.current==='mouse')setOpen(true);else setOpen(value=>!value);}}
   onKeyDown={e=>{if(e.key==='ArrowDown'||e.key==='ArrowUp'){e.preventDefault();const index=e.key==='ArrowUp'?items.length-1:0;if(open)container.current?.querySelectorAll<HTMLButtonElement>('.settings-nav-item')[index]?.focus();else{focusIndex.current=index;setOpen(true);}}}}>
   <span className="app-nav-icon"><Icon name={icon}/></span><span>{label}</span>
  </button>
  {open&&<div className="settings-nav-popover"><div id={section+'-navigation'} className="settings-nav-menu" role="group" aria-label={'Подразделы: '+label}
   onKeyDown={e=>{if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key))return;e.preventDefault();const buttons=Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('button')),current=buttons.indexOf(document.activeElement as HTMLButtonElement),next=e.key==='Home'?0:e.key==='End'?buttons.length-1:(current+(e.key==='ArrowDown'?1:-1)+buttons.length)%buttons.length;buttons[next]?.focus();}}>
   {items.map(([key,title])=><button key={key} type="button" className="settings-nav-item" aria-current={active&&type===key?'page':undefined} onClick={()=>{if(onSelect(key)){setOpen(false);trigger.current?.focus();}}}>{title}</button>)}
  </div></div>}
 </div>;
}
