import {candidateDb,candidateId,candidateHash} from '../repositories/vacancy-candidates.js';
import {boundGeneration} from '../repositories/generation-bound-db.js';
import {withDetailLease} from './vacancy-detail-leases.js';
import {importError,analyzeVacancyLeafRoles} from './vacancy-leaf-roles.js';
import {prepareVacancyImport,importVacancyCandidate,getVacancyImportHistory,publicImportPlan} from './vacancy-import.js';
import {jsonResponse} from '../http.js';
import {readClassificationInput} from '../repositories/classification-prompts.js';
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
async function reviewHash(plan){return candidateHash(JSON.stringify({candidateId:plan.candidateId,status:plan.status,targetId:plan.targetId,companyId:plan.companyId,changes:plan.changes,roles:plan.roleIds,snapshotId:plan._data?.x.snapshot.id,extractionId:plan._data?.x.extraction.id,roleInputHash:plan._data?.x.inputHash,sources:plan.sourceUrls,resolution:plan.resolution}));}
export async function readImportBatch(db,id){db=await candidateDb(db);const row=await db.prepare('SELECT b.*,(SELECT expires_at FROM vacancy_detail_leases WHERE lease_key=? AND generation=b.generation) leaseUntil FROM vacancy_import_batches b WHERE b.id=? AND b.generation=?').bind('import-batch:'+id,id,boundGeneration(db)).first();if(!row)throw importError('not_found','Пакет не найден в текущем поколении базы.',404);const items=JSON.parse(row.items_json);return {id:row.id,state:row.state==='running'&&!(row.leaseUntil>Date.now())?'paused':row.state,createdAt:row.created_at,items,completed:items.filter(i=>i.outcome==='complete').length,total:items.length};}
export async function previewVacancyImports(env,input){
 if(!input||Array.isArray(input)||Object.keys(input).some(k=>!['candidateIds','resolutions','requestId'].includes(k))||!Array.isArray(input.candidateIds)||!input.candidateIds.length||input.candidateIds.length>20||!uuid.test(input.requestId||''))throw importError('input','Нужны requestId и от1до20 ID первичных вакансий.',400);
 const ids=[...new Set(input.candidateIds.map(candidateId))],resolutions=input.resolutions||{};if(typeof resolutions!=='object'||Array.isArray(resolutions)||Object.keys(resolutions).some(k=>!ids.includes(Number(k))))throw importError('input','Некорректные разрешения совпадений.',400);
 const db=await candidateDb(env.DB),plans=[];for(const id of ids){const plan=await prepareVacancyImport({...env,DB:db},id,resolutions[id]||{});const hash=await reviewHash(plan);plans.push({...publicImportPlan(plan),_reviewHash:hash});}
 const items=[];for(const p of plans)items.push({id:p.candidateId,resolution:p.resolution,reviewHash:p._reviewHash,operationId:crypto.randomUUID(),outcome:['create','update','unchanged'].includes(p.status)?'pending':'blocked',action:p.status,reason:p.reason||null});
 const key=await candidateHash(JSON.stringify([input.requestId,items.map(i=>[i.id,i.reviewHash])])),id=crypto.randomUUID();await db.prepare("INSERT OR IGNORE INTO vacancy_import_batches(id,generation,request_key,items_json,state,created_at) VALUES(?,?,?,?,'preview',?)").bind(id,boundGeneration(db),key,JSON.stringify(items),new Date().toISOString()).run();const row=await db.prepare('SELECT id FROM vacancy_import_batches WHERE generation=? AND request_key=?').bind(boundGeneration(db),key).first();
 const summary={create:0,update:0,unchanged:0,possible_duplicate:0,blocked:0};for(const p of plans)summary[p.status in summary?p.status:'blocked']++;
 return {batch:await readImportBatch(db,row.id),plans:plans.map(publicImportPlan),summary};
}
export async function confirmVacancyImportBatch(env,id){const db=await candidateDb(env.DB);await readImportBatch(db,id);await db.prepare("UPDATE vacancy_import_batches SET state='pending' WHERE id=? AND generation=? AND state='preview'").bind(id,boundGeneration(db)).run();return readImportBatch(db,id);}
export async function runVacancyImportBatch(env,id,{sliceMs=24000}={}){
 const base=await candidateDb(env.DB);return withDetailLease(base,'import-batch:'+id,async db=>{
  const batch=await readImportBatch(db,id);if(batch.state==='preview')throw importError('confirmation','Сначала подтвердите предпросмотр пакета.');if(batch.state==='completed')return batch;
  await db.prepare("UPDATE vacancy_import_batches SET state='running' WHERE id=? AND generation=?").bind(id,boundGeneration(db)).run();
  const deadline=Date.now()+Math.min(sliceMs,120000);let processed=0;
  for(const item of batch.items){if(item.outcome==='complete'||item.outcome==='blocked')continue;if(processed>=3||Date.now()+100>=deadline)break;processed++;
   try{
    const receipt=await db.prepare("SELECT * FROM vacancy_import_operations WHERE id=? AND status='success'").bind(item.operationId).first();
    if(receipt){item.outcome='complete';item.result={action:receipt.action,vacancyId:receipt.vacancy_id,reused:true};}
    else{const plan=await prepareVacancyImport({...env,DB:db},item.id,item.resolution);if(await reviewHash(plan)!==item.reviewHash)throw importError('preview_changed','Результат отличается от подтверждённого предпросмотра. Проверьте эту запись заново.');item.result=await importVacancyCandidate({...env,DB:db},item.id,{token:plan.token,resolution:item.resolution,operationId:item.operationId,batchId:id});item.outcome='complete';item.reason=null;}
   }catch(e){if(['restored','detail_changed'].includes(e.code))throw e;item.outcome=e.code==='detail_busy'?'pending':'error';item.reason=e.status?e.message:'Обработка прервана; проверьте историю и продолжите пакет.';item.code=e.code||'storage';}
   await db.prepare('UPDATE vacancy_import_batches SET items_json=? WHERE id=? AND generation=?').bind(JSON.stringify(batch.items),id,boundGeneration(db)).run();
  }
  const state=batch.items.every(i=>['complete','blocked'].includes(i.outcome))?'completed':batch.items.some(i=>i.outcome==='error')?'paused':'pending';await db.prepare('UPDATE vacancy_import_batches SET state=?,items_json=? WHERE id=? AND generation=?').bind(state,JSON.stringify(batch.items),id,boundGeneration(db)).run();return readImportBatch(db,id);
 },{ttl:150000});
}
export function startImportBatch(env,ctx,id){if(!ctx?.waitUntil)return 'manual_resume';ctx.waitUntil(runVacancyImportBatch(env,id).catch(()=>{}));return 'background';}
export async function vacancyImportDashboard(db){db=await candidateDb(db);const [c,b]=await db.batch([db.prepare('SELECT c.id,c.title_raw title,c.company_id companyId,i.company_name companyName,c.main_vacancy_id mainVacancyId FROM vacancy_candidates c LEFT JOIN company_identity i ON i.id=c.company_id WHERE EXISTS(SELECT 1 FROM vacancy_raw_snapshots s WHERE s.candidate_id=c.id AND s.content_status=\'valid\') ORDER BY c.id DESC LIMIT 50'),db.prepare('SELECT id FROM vacancy_import_batches WHERE generation=? ORDER BY created_at DESC,rowid DESC LIMIT 20').bind(boundGeneration(db))]);const batches=[];for(const row of b.results||[])batches.push(await readImportBatch(db,row.id));return {candidates:c.results||[],batches,automaticImport:false,batchLimit:20};}
export async function vacancyImportRoute(request,env,url,user,ctx){
 if(!user.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);if(!env.DB)return jsonResponse({error:'Хранилище не подключено.'},503);if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
 try{
 if(url.pathname==='/api/vacancy-import'&&request.method==='GET')return jsonResponse(await vacancyImportDashboard(env.DB));
 let match=url.pathname.match(/^\/api\/vacancy-import\/(roles|history)\/(\d+)$/);
 if(match?.[1]==='history'&&request.method==='GET')return jsonResponse({history:await getVacancyImportHistory(env.DB,Number(match[2]))});
 if(match?.[1]==='roles'&&request.method==='POST'){if(Object.keys(await readClassificationInput(request)).length)throw importError('input','Передайте пустой объект.',400);return jsonResponse(await analyzeVacancyLeafRoles(env,Number(match[2]),{signal:request.signal}));}
 if(url.pathname==='/api/vacancy-import/preview'&&request.method==='POST')return jsonResponse(await previewVacancyImports(env,await readClassificationInput(request)));
 match=url.pathname.match(/^\/api\/vacancy-import\/batches\/([0-9a-f-]{36})(?:\/(confirm|run))?$/);
 if(match&&request.method==='GET'&&!match[2])return jsonResponse(await readImportBatch(env.DB,match[1]));
 if(match&&request.method==='POST'&&match[2]){const input=await readClassificationInput(request);if(match[2]==='confirm'){if(input.confirmed!==true||Object.keys(input).length!==1)throw importError('confirmation','Подтвердите импорт просмотренного пакета.',400);const batch=await confirmVacancyImportBatch(env,match[1]);return jsonResponse({batch,execution:startImportBatch(env,ctx,batch.id)},202);}if(Object.keys(input).length)throw importError('input','Передайте пустой объект.',400);return jsonResponse(await runVacancyImportBatch(env,match[1],{sliceMs:120000}));}
 return jsonResponse({error:'Метод не поддерживается.'},405);
 }catch(e){return jsonResponse({error:e.status?e.message:'Не удалось выполнить безопасный импорт.',code:e.code},e.status||500);}
}
