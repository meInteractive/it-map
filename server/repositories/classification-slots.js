import {CLASSIFICATION_LIMITS} from '../services/classification-config.js';
import {boundGeneration} from './generation-bound-db.js';
import {CLASSIFICATION_JOB_SCHEMA} from './classification-job-schema.js';
export async function withClassificationSlot(env,work){
 const db=env.DB,owner=crypto.randomUUID(),generation=boundGeneration(db);let slot;
 await db.batch(CLASSIFICATION_JOB_SCHEMA.map(sql=>db.prepare(sql)));
 for(const index of [1,2]){
  const claimed=await db.prepare(`INSERT INTO vacancy_classification_slots(slot,generation,owner_token,expires_at) VALUES(?,?,?,?) ON CONFLICT(slot) DO UPDATE SET generation=excluded.generation,owner_token=excluded.owner_token,expires_at=excluded.expires_at WHERE vacancy_classification_slots.generation<>excluded.generation OR vacancy_classification_slots.expires_at<=CAST(julianday('now')*86400000-210866760000000 AS INTEGER)`).bind(index,generation,owner,Date.now()+CLASSIFICATION_LIMITS.modelLeaseMs).run();
  if(claimed.meta?.changes){slot=index;break;}
 }
 if(!slot)throw Object.assign(new Error('Одновременно уже выполняются две классификации. Продолжите пакет позже.'),{code:'classification_busy',status:409});
 let uncertain=false;try{return await work();}catch(error){uncertain=error.code==='network'||error.name==='TimeoutError'||error.name==='AbortError';throw error;}finally{if(!uncertain)await db.prepare('DELETE FROM vacancy_classification_slots WHERE slot=? AND generation=? AND owner_token=?').bind(slot,generation,owner).run().catch(()=>{});}
}
