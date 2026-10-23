import {agencyCandidateDb} from '../repositories/parser-owner.js';
import {boundGeneration,generationBoundDb} from '../repositories/generation-bound-db.js';
export async function withDetailLease(db,key,work,{ttl=300000}={}){
 if(/^candidate:\d+$/.test(key))db=await agencyCandidateDb(db,Number(key.slice(10)));
 const owner=crypto.randomUUID(),generation=boundGeneration(db),now=Date.now();
 const claim=key.startsWith('llm:')?'vacancy_detail_leases.expires_at<=?':'vacancy_detail_leases.generation<>excluded.generation OR vacancy_detail_leases.expires_at<=?';
 const result=await db.prepare(`INSERT INTO vacancy_detail_leases(lease_key,generation,owner_token,expires_at) VALUES(?,?,?,?) ON CONFLICT(lease_key) DO UPDATE SET generation=excluded.generation,owner_token=excluded.owner_token,expires_at=excluded.expires_at WHERE ${claim}`).bind(key,generation,owner,now+ttl,now).run();
 if(!result.meta?.changes)throw Object.assign(new Error('Эта операция уже выполняется. Повторите позже.'),{code:'detail_busy',status:409});
 const owned=generationBoundDb(db,generation,{sql:"EXISTS(SELECT 1 FROM vacancy_detail_leases WHERE lease_key=? AND generation=? AND owner_token=? AND expires_at>CAST(julianday('now')*86400000-210866760000000 AS INTEGER))",bindings:[key,generation,owner],failure:()=>Object.assign(new Error('Обработчик потерял блокировку.'),{code:'detail_changed',status:409})});
 let uncertain=false;try{return await work(owned);}catch(e){uncertain=['network','timeout'].includes(e.code)||['TimeoutError','AbortError'].includes(e.name);throw e;}finally{if(!uncertain)await db.prepare('DELETE FROM vacancy_detail_leases WHERE lease_key=? AND generation=? AND owner_token=?').bind(key,generation,owner).run().catch(()=>{});}
}
