// Server-only; extracted from the v218 Worker.
import {normalizeCatalog} from './catalog.js';
import {DATA} from '../data.js';
import {CRM_STATUSES} from '../constants.js';

export async function readCatalogProjectionSource(db){
 const row=await db.prepare('SELECT data_json AS dataJson FROM app_catalog WHERE id=1').first();
 return normalizeCatalog(row?.dataJson?JSON.parse(row.dataJson):DATA);
}

export async function readCompactEntities(db,userName,view='list',entityType='company'){
 const catalog=await readCatalogProjectionSource(db);
 const items=entityType==='agency'?catalog.agencies:entityType==='all'?[...catalog.companies,...catalog.agencies]:catalog.companies;
 // One bulk insert per identity type supports catalogs imported before stable IDs existed.
 const identityTable=entityType==='agency'?'agency_identity':'company_identity';
 const identityColumn=entityType==='agency'?'agency_name':'company_name';
 if(entityType!=='all')await db.prepare(`INSERT OR IGNORE INTO ${identityTable} (${identityColumn}) SELECT value FROM json_each(?)`).bind(JSON.stringify(items.map(c=>c.name))).run();
 if(entityType==='all')await db.batch([db.prepare('INSERT OR IGNORE INTO company_identity(company_name) SELECT value FROM json_each(?)').bind(JSON.stringify(catalog.companies.map(c=>c.name))),db.prepare('INSERT OR IGNORE INTO agency_identity(agency_name) SELECT value FROM json_each(?)').bind(JSON.stringify(catalog.agencies.map(c=>c.name)))]);
 const results=await db.batch([
  db.prepare(`SELECT i.id,i.company_name AS name,'company' AS entityType,COALESCE(s.favorite,0) AS favorite,COALESCE(s.hidden,0) AS hidden FROM company_identity i LEFT JOIN company_user_state s ON s.company_name=i.company_name AND s.user_name=? UNION ALL SELECT i.id,i.agency_name AS name,'agency' AS entityType,COALESCE(s.favorite,0),COALESCE(s.hidden,0) FROM agency_identity i LEFT JOIN company_user_state s ON s.company_name=i.agency_name AND s.user_name=?`).bind(userName,userName),
  db.prepare("SELECT company_name AS name FROM company_vacancies UNION SELECT real_company_name AS name FROM company_vacancies WHERE real_company_name IS NOT NULL AND real_company_name<>''"),
  db.prepare(`SELECT company_name AS name,status FROM (SELECT company_name,status,ROW_NUMBER() OVER(PARTITION BY company_name ORDER BY (vacancy_id IS NOT NULL) DESC,CASE status ${CRM_STATUSES.map((s,i)=>`WHEN '${s.replaceAll("'","''")}' THEN ${s==='Отказ'?0:s?i:-1}`).join(' ')} ELSE -1 END DESC,updated_at DESC,id DESC) AS position FROM company_applications WHERE user_name=?) WHERE position=1`).bind(userName),
  db.prepare('SELECT company_name AS name FROM company_vacancy_state')
 ]);
 const state=new Map((results[0].results||[]).map(r=>[r.entityType+':'+r.name,r]));
 const vacancies=new Set((results[1].results||[]).map(r=>r.name));
 const applications=new Map((results[2].results||[]).map(r=>[r.name,r.status||null]));
 const initialized=new Set((results[3].results||[]).map(r=>r.name));
 return items.map(c=>{
  const type=c.entityType||'company',s=state.get(type+':'+c.name)||{};
  const base={id:s.id??null,name:c.name,rank:c.rank,entityType:type,favorite:!!s.favorite,hidden:!!s.hidden,hasVacancies:vacancies.has(c.name)||(!initialized.has(c.name)&&(c.jobs||[]).some(j=>j.title&&/^https?:\/\//.test(j.url||''))),hasApplications:applications.has(c.name),applicationStatus:applications.get(c.name)||null};
  if(view==='index')return {...base,...catalogVacancyFields(c)};
  if(view==='map')return {...base,offices:(c.offices||[]).map(({address,metro,lat,lon})=>({address,metro,lat,lon}))};
  const row={...base,offices:(c.offices||[]).map(({address,metro})=>({address,metro})),middle:c.middle,senior:c.senior,lead:c.lead,head:c.head,deferment:c.deferment??'',rating:c.rating??null,wlbScore:c.wlbScore??null,hasVacancySource:Object.values(c.vacancySources||{}).some(value=>typeof value==='string'&&value.trim())};
  if(type==='agency')Object.assign(row,{reviewsScore:c.reviewsScore,salaryRangeSource:c.salaryRangeSource,revenue:{amount:c.revenue?.amount??null},functionalSpecializations:c.functionalSpecializations});
  return row;
 });
}

export async function readCompanyDetail(db,userName,id,name,entityType='company'){
 const catalog=await readCatalogProjectionSource(db);
 const agency=entityType==='agency';
 const identity=id?await db.prepare(agency?'SELECT agency_name AS name FROM agency_identity WHERE id=?':'SELECT company_name AS name FROM company_identity WHERE id=?').bind(id).first():null;
 const company=(agency?catalog.agencies:catalog.companies).find(c=>c.name===(identity?.name||name));
 if(!company)return null;
 const state=await db.prepare('SELECT favorite,hidden FROM company_user_state WHERE user_name=? AND company_name=?').bind(userName,company.name).first();
 const count=await db.prepare('SELECT COUNT(*) AS total FROM company_vacancies WHERE company_name=? OR real_company_name=?').bind(company.name,company.name).first();
 const {jobs,...details}=company;
 return {...details,id:id||null,favorite:!!state?.favorite,hidden:!!state?.hidden,vacancyCount:Number(count?.total||0)};
}

export function catalogVacancyFields(c){return {middle:c.middle,senior:c.senior,lead:c.lead,head:c.head,applicationUrl:c.applicationUrl};}

export async function readEntityDirectory(db,names,forVacancies=false){
 const catalog=await readCatalogProjectionSource(db),wanted=new Set(names);
 return [...catalog.companies,...catalog.agencies].filter(c=>wanted.has(c.name)).map(c=>({name:c.name,rank:c.rank,entityType:c.entityType,...(forVacancies?catalogVacancyFields(c):{})}));
}

export async function searchCatalogNames(db,query){
 const catalog=await readCatalogProjectionSource(db),q=query.toLowerCase();
 return catalog.companies.filter(c=>(c.name+' '+c.sector+' '+c.roles+' '+(c.offices||[]).map(o=>o.metro+' '+o.address).join(' ')+' '+(c.jobs||[]).map(j=>j.title).join(' ')+' '+(c.revenue?.legal||'')+' '+(c.revenue?.metric||'')+' '+Object.values(c.vpnAi||{}).join(' ')).toLowerCase().includes(q)).map(c=>({name:c.name,sector:c.sector}));
}
