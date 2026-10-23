import {sign,timingSafeEqual,bytesToBase64Url,base64UrlToBytes} from '../auth/session.js';
import {jsonResponse,securityHeaders} from '../http.js';
import {generationBoundDb} from '../repositories/generation-bound-db.js';
const scope=':d1-raw-export:v1';
const quote=name=>'"'+String(name).replaceAll('"','""')+'"';
const schemaQuery="SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' AND substr(name,1,4)<>'_cf_' ORDER BY type,name";

// This credential is scoped to one read-only endpoint and expires in five
// minutes. It is issued only after the existing MCP cloud-owner authorization.
export async function databaseExportTicket(env,ownerId){
 const generation=await env.DB.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').first();
 const expiresAt=Date.now()+300000;
 const data=bytesToBase64Url(new TextEncoder().encode(JSON.stringify({ownerId,generation:generation?.generation,expiresAt})));
 return {path:'/api/internal/database-export',token:data+'.'+await sign(env.SESSION_SECRET+scope,data),expiresAt};
}

export async function readDirectDatabaseSnapshot(db){
 // Trusted identifiers come from the LIVE schema, never a repository allowlist
 // or user backup. No bootstrap, migration, filtering of operational tables,
 // normalization, truncation or writes are performed.
 const schema=(await db.prepare(schemaQuery).all()).results||[];
 const definitions=[];
 for(const table of schema.filter(o=>o.type==='table')){
  const columns=(await db.prepare(`PRAGMA table_xinfo(${quote(table.name)})`).all()).results||[];
  definitions.push({name:table.name,columns:columns.filter(c=>c.hidden!==1)});
 }
 const hasSequence=await db.prepare("SELECT 1 FROM sqlite_schema WHERE name='sqlite_sequence'").first();
 const statements=definitions.map(t=>db.prepare(`SELECT json_array(${t.columns.map(c=>{
  const col=quote(c.name);
  return `json_array(typeof(${col}),CASE typeof(${col}) WHEN 'null' THEN NULL WHEN 'blob' THEN hex(${col}) WHEN 'real' THEN printf('%!.17g',${col}) ELSE CAST(${col} AS TEXT) END)`;
 }).join(',')}) AS cells FROM ${quote(t.name)}`));
 for(const table of definitions)statements.push(db.prepare(`PRAGMA foreign_key_list(${quote(table.name)})`));
 statements.push(db.prepare(schemaQuery),db.prepare(hasSequence?'SELECT name,seq FROM sqlite_sequence ORDER BY name':'SELECT 1 WHERE 0'),db.prepare('PRAGMA foreign_key_check'));
 const results=await db.batch(statements),n=definitions.length;
 const capturedSchema=results[2*n].results||[];
 if(JSON.stringify(schema)!==JSON.stringify(capturedSchema))throw new Error('Схема изменилась во время экспорта. Повторите выгрузку.');
 if(results[2*n+2].results?.length)throw new Error('В исходной базе нарушены внешние ключи; выгрузка остановлена.');
 return {format:'it-map-direct-d1-snapshot',version:1,createdAt:new Date().toISOString(),schema:capturedSchema,
  sequences:results[2*n+1].results||[],tables:definitions.map((t,i)=>({...t,foreignKeys:results[n+i].results||[],rows:(results[i].results||[]).map(row=>JSON.parse(row.cells))}))};
}

export async function databaseExportRoute(request,env){
 if(request.method!=='GET')return jsonResponse({error:'Use GET'},405);
 try{
  const authorization=request.headers.get('authorization')||'';
  const token=authorization.startsWith('Bearer ')?authorization.slice(7):'';
  const parts=token.split('.');
  if(parts.length!==2||!env.SESSION_SECRET||!timingSafeEqual(parts[1],await sign(env.SESSION_SECRET+scope,parts[0])))return jsonResponse({error:'Недействительное разрешение экспорта.'},401);
  const data=JSON.parse(new TextDecoder().decode(base64UrlToBytes(parts[0])));
  if(!Number.isSafeInteger(data.expiresAt)||data.expiresAt<=Date.now()||data.expiresAt>Date.now()+300000)return jsonResponse({error:'Разрешение экспорта истекло.'},401);
  const [settings,generation]=await Promise.all([env.DB.prepare('SELECT scheduler_json FROM firecrawl_settings WHERE id=1').first(),env.DB.prepare('SELECT generation FROM app_runtime_generation WHERE id=1').first()]);
  if(!data.ownerId||JSON.parse(settings?.scheduler_json||'{}').cloudOwnerId!==data.ownerId||generation?.generation!==data.generation)return jsonResponse({error:'Привязка владельца или поколение базы изменились.'},403);
  const guarded=generationBoundDb(env.DB,data.generation,{sql:"EXISTS(SELECT 1 FROM firecrawl_settings WHERE id=1 AND json_extract(scheduler_json,'$.cloudOwnerId')=?)",bindings:[data.ownerId],failure:()=>Object.assign(new Error('Владелец изменился.'),{status:403})});
  const snapshot=await readDirectDatabaseSnapshot(guarded);
  return new Response(JSON.stringify(snapshot),{headers:{...securityHeaders,'content-type':'application/json; charset=utf-8','content-disposition':'attachment; filename="it-map-direct-d1.json"'}});
 }catch{return jsonResponse({error:'Не удалось выгрузить полную D1. Частичный файл не создан.'},500);}
}
