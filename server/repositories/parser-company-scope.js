import {parserOwner,parserOwnerScopeSql} from './parser-owner.js';
import {candidateId} from './vacancy-candidates.js';
import {generationBoundDb,boundGeneration} from './generation-bound-db.js';
// Evaluate the current toggle and the requesting administrator's own favorites.
// Used as a transaction guard too, so changes during a run stop further writes.
export const parserCompanyScopeSql=`(coalesce((SELECT favorites_only FROM firecrawl_settings WHERE id=1),1)=0 OR EXISTS(SELECT 1 FROM company_identity i JOIN company_user_state u ON u.company_name=i.company_name WHERE i.id=? AND u.user_name=? AND u.favorite=1))`;
export const parserScopeError=()=>Object.assign(new Error('Включено «Только избранные компании». Добавьте компанию в избранное или отключите настройку в Парсере.'),{status:403,code:'favorites_only'});
export async function parserCompanyDb(db,companyId,userName){
 const owner=parserOwner(companyId),bindings=[owner.companyId,owner.agencyId,userName??''];
 if(owner.type==='agency'){
  const sql="EXISTS(SELECT 1 FROM firecrawl_settings WHERE id=1 AND coalesce(json_extract(scheduler_json,'$.agenciesEnabled'),0)=1)",failure=()=>Object.assign(new Error('Парсинг агентств выключен. Прогресс сохранён до включения.'),{status:403,code:'agencies_disabled'});
  if(!(await db.prepare(`SELECT ${sql} allowed`).first())?.allowed)throw failure();
  db=generationBoundDb(db,boundGeneration(db),{sql,bindings:[],failure});
 }

 const sql=`EXISTS(SELECT 1 FROM (SELECT ? AS company_id,? AS agency_id) po WHERE ${parserOwnerScopeSql('po')})`;
 const failure=()=>Object.assign(new Error(owner.type==='agency'?'Парсинг агентств выключен или агентство исключено фильтром избранного.':'Включено «Только избранные компании». Добавьте компанию в избранное или отключите настройку в Парсере.'),{status:403,code:'favorites_only'});
 const row=await db.prepare(`SELECT ${sql} AS allowed`).bind(...bindings).first();
 if(!row?.allowed)throw failure();
 return generationBoundDb(db,boundGeneration(db),{sql,bindings,failure});
}
