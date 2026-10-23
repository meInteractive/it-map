import {useState,type Dispatch,type SetStateAction} from 'react';
// Preserve visited-section controls while unmounting inactive sections and their API subscribers.
const values=new Map<string,unknown>();
export function useSectionState<T>(key:string,initial:T):[T,Dispatch<SetStateAction<T>>]{
 const [value,setValue]=useState<T>(()=>values.has(key)?values.get(key) as T:initial);
 return [value,next=>setValue(current=>{const updated=typeof next==='function'?(next as (v:T)=>T)(current):next;values.set(key,updated);return updated;})];
}
