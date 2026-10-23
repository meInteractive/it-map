// Existing company calls retain their numeric argument; agencies use an explicit
// reference. IDs in the two identity tables never share an implicit namespace.
export function parserOwner(value){
 const object=value&&typeof value==='object',companyId=object?(value.companyId??value.company_id??null):value,agencyId=object?(value.agencyId??value.agency_id??null):null;
 const company=companyId==null?null:Number(companyId),agency=agencyId==null?null:Number(agencyId);
 if((company!==null)+(agency!==null)!==1||![company,agency].every(n=>n===null||Number.isSafeInteger(n)&&n>0))throw Object.assign(new Error('Укажите ровно одну компанию или агентство.'),{status:400,code:'owner_input'});
 const type=agency!==null?'agency':'company',id=agency??company;
 return {companyId:company,agencyId:agency,id,type,column:agency!==null?'agency_id':'company_id',key:type+':'+id,identityTable:agency!==null?'agency_identity':'company_identity',nameColumn:agency!==null?'agency_name':'company_name'};
}
export const parserOwnerKey=value=>parserOwner(value).key;
export const parserOwnerColumns=value=>{const o=parserOwner(value);return {companyId:o.companyId,agencyId:o.agencyId};};
export const parserOwnerSelect=(alias='')=>`coalesce('agency:'||${alias}agency_id,'company:'||${alias}company_id)`;
export const parserOwnerName=(alias='')=>`CASE WHEN ${alias}agency_id IS NOT NULL THEN (SELECT agency_name FROM agency_identity WHERE id=${alias}agency_id) ELSE (SELECT company_name FROM company_identity WHERE id=${alias}company_id) END`;
// Exactly one bound user name, regardless of which owner is selected.
export function parserOwnerScopeSql(alias){
 const c=alias+'.company_id',a=alias+'.agency_id',name=parserOwnerName(alias+'.');
 return `EXISTS(SELECT 1 FROM firecrawl_settings pf WHERE pf.id=1 AND ${name} IS NOT NULL AND ((${a} IS NULL AND ${c} IS NOT NULL) OR (${a} IS NOT NULL AND ${c} IS NULL AND coalesce(json_extract(pf.scheduler_json,'$.agenciesEnabled'),0)=1)) AND (pf.favorites_only=0 OR EXISTS(SELECT 1 FROM company_user_state pu WHERE pu.company_name=${name} AND pu.user_name=? AND pu.favorite=1)))`;
}

// Manual stage endpoints must honor the same agency switch as the scheduler.
// A transaction guard fences provider results when the switch changes in flight.
export async function agencyCandidateDb(db,id){
 const row=await db.prepare('SELECT agency_id FROM vacancy_candidates WHERE id=?').bind(id).first();
 if(!row?.agency_id)return db;
 const {generationBoundDb,boundGeneration}=await import('./generation-bound-db.js');
 const sql="EXISTS(SELECT 1 FROM vacancy_candidates c JOIN firecrawl_settings f ON f.id=1 WHERE c.id=? AND c.agency_id=? AND c.company_id IS NULL AND coalesce(json_extract(f.scheduler_json,'$.agenciesEnabled'),0)=1)",bindings=[id,row.agency_id];
 const failure=()=>Object.assign(new Error('Парсинг агентств выключен. Сохранённый прогресс можно продолжить после включения.'),{code:'agencies_disabled',status:409});
 if(!(await db.prepare(`SELECT ${sql} allowed`).bind(...bindings).first())?.allowed)throw failure();
 return generationBoundDb(db,boundGeneration(db),{sql,bindings,failure});
}
