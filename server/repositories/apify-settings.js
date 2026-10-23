import {candidateDb,candidateHash} from './vacancy-candidates.js';
import {encryptGoogleToken,decryptGoogleToken} from '../integrations/google-calendar.js';
import {APIFY_PLANS,apifyProfile,apifyError} from '../integrations/apify.js';
export async function apifySettings(db){db=await candidateDb(db);await db.prepare('INSERT OR IGNORE INTO apify_settings(id) VALUES(1)').run();return db.prepare('SELECT * FROM apify_settings WHERE id=1').first();}
export function publicApifySettings(s){return {enabled:!!s.enabled,paidAllowed:!!s.paid_allowed,revision:s.revision,profiles:Object.fromEntries(['free','starter'].map(profile=>{let account=null;try{account=JSON.parse(s[profile+'_account_json']||'null');}catch{}return [profile,{configured:!!s[profile+'_key_encrypted'],keyMask:s[profile+'_key_suffix']?'••••'+s[profile+'_key_suffix']:'',account,lastError:s[profile+'_error'],limits:APIFY_PLANS[profile]}];}))};}
export async function apifyToken(env,s,profile){apifyProfile(profile);if(!env.SESSION_SECRET||!s[profile+'_key_encrypted'])throw apifyError('apify_disabled',400);try{return await decryptGoogleToken(env.SESSION_SECRET+':apify:v1',s[profile+'_key_encrypted']);}catch{throw apifyError('apify_auth',400);}}
export async function apifyTokenHash(env,s,profile){return candidateHash(await apifyToken(env,s,profile));}
export async function saveApifyKey(env,input){
 const profile=apifyProfile(input.profile);if(Object.keys(input).some(k=>!['profile','apiKey','revision'].includes(k))||!Number.isSafeInteger(input.revision)||typeof input.apiKey!=='string'||!/^apify_api_[A-Za-z0-9_-]{12,512}$/.test(input.apiKey.trim())||!env.SESSION_SECRET)throw apifyError('apify_data',400);
 const token=input.apiKey.trim(),encrypted=await encryptGoogleToken(env.SESSION_SECRET+':apify:v1',token);
 const r=await env.DB.prepare(`UPDATE apify_settings SET ${profile}_key_encrypted=?,${profile}_key_suffix=?,${profile}_account_json='',${profile}_error='',paid_allowed=0,revision=revision+1 WHERE id=1 AND revision=?`).bind(encrypted,token.slice(-4),input.revision).run();if(!r.meta?.changes)throw apifyError('apify_changed');return publicApifySettings(await apifySettings(env.DB));
}
export async function saveApifySettings(db,input){
 if(Object.keys(input).some(k=>!['enabled','paidAllowed','confirmed','revision'].includes(k))||typeof input.enabled!=='boolean'||typeof input.paidAllowed!=='boolean'||!Number.isSafeInteger(input.revision))throw apifyError('apify_data',400);
 const s=await apifySettings(db);if(input.enabled&&!s.free_key_encrypted&&!(input.paidAllowed&&s.starter_key_encrypted))throw apifyError('apify_disabled',400);
 if(input.paidAllowed&&(!s.starter_key_encrypted||!s.paid_allowed&&input.confirmed!==true))throw apifyError('apify_data',400);
 const r=await db.prepare('UPDATE apify_settings SET enabled=?,paid_allowed=?,revision=revision+1 WHERE id=1 AND revision=?').bind(input.enabled?1:0,input.paidAllowed?1:0,input.revision).run();if(!r.meta?.changes)throw apifyError('apify_changed');return publicApifySettings(await apifySettings(db));
}
