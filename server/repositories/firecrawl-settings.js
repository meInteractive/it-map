import {encryptGoogleToken,decryptGoogleToken} from '../integrations/google-calendar.js';
import {FIRECRAWL_PLANS,firecrawlMode} from '../integrations/firecrawl-plans.js';
import {candidateHash} from './vacancy-candidates.js';
import {fcError} from '../integrations/firecrawl.js';
import {candidateDb} from './vacancy-candidates.js';
import {generationBoundDb,boundGeneration} from './generation-bound-db.js';
export async function firecrawlSettings(db){db=await candidateDb(db);await db.prepare('INSERT OR IGNORE INTO firecrawl_settings(id) VALUES(1)').run();return db.prepare('SELECT * FROM firecrawl_settings WHERE id=1').first();}
export const profileColumns=mode=>firecrawlMode(mode)==='free'?{key:'key_encrypted',suffix:'key_suffix',hash:'free_key_hash',status:'connection_status',error:'last_error',checked:'checked_at',usage:'free_usage_json'}:{key:'hobby_key_encrypted',suffix:'hobby_key_suffix',hash:'hobby_key_hash',status:'hobby_connection_status',error:'hobby_last_error',checked:'hobby_checked_at',usage:'hobby_usage_json'};
export function publicFirecrawlSettings(s){const c=profileColumns(s.mode);const profiles=Object.fromEntries(['free','hobby'].map(mode=>{const f=profileColumns(mode);return [mode,{configured:!!s[f.key],keyMask:s[f.suffix]?'••••'+s[f.suffix]:'',connectionStatus:s[f.status],lastError:s[f.error],checkedAt:s[f.checked]}];}));return {configured:!!s[c.key],keyMask:s[c.suffix]?'••••'+s[c.suffix]:'',enabled:!!s.enabled,favoritesOnly:s.favorites_only!==0,mode:s.mode,maxPages:s.max_pages,concurrency:1,timeoutMs:s.timeout_ms,proxy:s.mode==='free'?'basic':s.proxy,enhancedAllowed:s.mode==='hobby'&&!!s.enhanced_allowed,revision:s.revision,connectionStatus:s[c.status],lastError:s[c.error],checkedAt:s[c.checked],profiles,sameKey:!!s.free_key_hash&&s.free_key_hash===s.hobby_key_hash,limits:FIRECRAWL_PLANS[s.mode]};}
export async function firecrawlKey(env,s,mode=s.mode){const c=profileColumns(mode);if(!s[c.key])throw fcError('missing_key',400);try{return await decryptGoogleToken(env.SESSION_SECRET+':firecrawl:v1',s[c.key]);}catch{throw fcError('missing_key',400);}}
export async function saveFirecrawlKey(env,input){
 if(Object.keys(input).some(k=>!['apiKey','revision','mode'].includes(k))||typeof input.apiKey!=='string'||!/^fc-[A-Za-z0-9_-]{12,512}$/.test(input.apiKey.trim())||!Number.isSafeInteger(input.revision))throw Object.assign(new Error('Введите ключ Firecrawl (fc-…) и текущую редакцию.'),{status:400});
 const mode=firecrawlMode(input.mode??'free'),c=profileColumns(mode);if(!env.SESSION_SECRET)throw fcError('missing_key',503);
 const encrypted=await encryptGoogleToken(env.SESSION_SECRET+':firecrawl:v1',input.apiKey.trim()),hash=await candidateHash(input.apiKey.trim());
 const downgrade=mode==='hobby'?",mode='free',proxy='basic',enhanced_allowed=0,enabled=CASE WHEN length(key_encrypted)>0 THEN enabled ELSE 0 END":'';
 const r=await env.DB.prepare(`UPDATE firecrawl_settings SET ${c.key}=?,${c.suffix}=?,${c.hash}=?,${c.status}='unchecked',${c.error}='',${c.checked}=NULL,${c.usage}='',revision=revision+1${downgrade} WHERE id=1 AND revision=?`).bind(encrypted,input.apiKey.trim().slice(-4),hash,input.revision).run();if(!r.meta?.changes)throw fcError('changed',409);
 return publicFirecrawlSettings(await firecrawlSettings(env.DB));
}
export async function saveFirecrawlSettings(db,input){
 if(input.favoritesOnly!==undefined&&typeof input.favoritesOnly!=='boolean')throw Object.assign(new Error('favoritesOnly должен быть boolean.'),{status:400});
 const keys=['enabled','maxPages','concurrency','timeoutMs','proxy','enhancedAllowed','revision'];
 if(Object.keys(input).some(k=>!keys.includes(k)&&k!=='favoritesOnly')||keys.some(k=>!(k in input))||typeof input.enabled!=='boolean'||typeof input.enhancedAllowed!=='boolean'||!Number.isSafeInteger(input.maxPages)||input.maxPages<1||input.maxPages>10||input.concurrency!==1||!Number.isSafeInteger(input.timeoutMs)||input.timeoutMs<5000||input.timeoutMs>60000||!['basic','enhanced'].includes(input.proxy)||input.proxy==='enhanced'&&!input.enhancedAllowed||!Number.isSafeInteger(input.revision))throw Object.assign(new Error('Некорректные настройки Firecrawl. Лимит 1–10 страниц, параллельность 1, тайм-аут 5–60 с.'),{status:400});
 const s=await firecrawlSettings(db);if((input.proxy!=='basic'||input.enhancedAllowed)&&s.mode!=='hobby')throw fcError('policy',400);if(input.enabled&&!s[profileColumns(s.mode).key])throw fcError('missing_key',400);
 const r=await db.prepare("UPDATE firecrawl_settings SET scheduler_json=json_set(scheduler_json,'$.revision',coalesce(json_extract(scheduler_json,'$.revision'),0)+CASE WHEN favorites_only<>? THEN 1 ELSE 0 END),enabled=?,favorites_only=?,max_pages=?,timeout_ms=?,proxy=?,enhanced_allowed=?,revision=revision+1 WHERE id=1 AND revision=?").bind(input.favoritesOnly===undefined?s.favorites_only:input.favoritesOnly?1:0,input.enabled?1:0,input.favoritesOnly===undefined?s.favorites_only:input.favoritesOnly?1:0,input.maxPages,input.timeoutMs,input.proxy,input.enhancedAllowed?1:0,input.revision).run();if(!r.meta?.changes)throw fcError('changed',409);
 return publicFirecrawlSettings(await firecrawlSettings(db));
}
export async function withFirecrawlLease(db,callback,ttlMs=600000){
 const owner=crypto.randomUUID(),generation=boundGeneration(db),now=Date.now();
 const r=await db.prepare(`INSERT INTO firecrawl_request_lease(id,generation,owner_token,expires_at) VALUES(1,?,?,?) ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,owner_token=excluded.owner_token,expires_at=excluded.expires_at WHERE firecrawl_request_lease.expires_at<=? OR firecrawl_request_lease.generation<>?`).bind(generation,owner,now+ttlMs,now,generation).run();
 if(!r.meta?.changes)throw fcError('busy',409);
 const assertOwned=async()=>{const row=await db.prepare('SELECT owner_token,expires_at FROM firecrawl_request_lease WHERE id=1 AND generation=?').bind(generation).first();if(row?.owner_token!==owner||row.expires_at<=Date.now())throw fcError('changed',409);};
 const ownedDb=generationBoundDb(db,generation,{sql:`EXISTS(SELECT 1 FROM firecrawl_request_lease WHERE id=1 AND generation=? AND owner_token=? AND expires_at>(julianday('now')-2440587.5)*86400000)`,bindings:[generation,owner],failure:()=>fcError('changed',409)});
 try{return await callback(assertOwned,ownedDb);}finally{try{await db.prepare('DELETE FROM firecrawl_request_lease WHERE id=1 AND generation=? AND owner_token=?').bind(generation,owner).run();}catch{/* A restore invalidates the old facade; it must not clear a new owner. */}}
}
