import {parserOwner,parserOwnerKey,parserOwnerName} from './parser-owner.js';
import {hhInput} from '../integrations/apify.js';
import {readAppCatalog,ensureCompanyIdentities,ensureAgencyIdentities} from './catalog.js';
import {VACANCY_SOURCE_META} from '../constants.js';
import {candidateId} from './vacancy-candidates.js';
import {safeListingUrl,publicHost,fcError} from '../integrations/firecrawl.js';
import {defaultListingAdapter,LISTING_ADAPTERS} from '../services/vacancy-listing-extractor.js';
export function validateListingConfig(input={}){
 const keys=['adapter','html','titleClass','detailPathPrefix','allowedHosts','pagination','useEnhancedProxy'];
 if(!input||typeof input!=='object'||Array.isArray(input)||Object.keys(input).some(k=>!keys.includes(k)))throw Object.assign(new Error('Некорректная конфигурация извлечения.'),{status:400});
 const value={useEnhancedProxy:input.useEnhancedProxy??false,adapter:input.adapter||'generic',html:input.html??false,titleClass:input.titleClass||'',detailPathPrefix:input.detailPathPrefix||'',allowedHosts:input.allowedHosts||[],pagination:input.pagination??null};
 if(typeof value.useEnhancedProxy!=='boolean'||!Object.hasOwn(LISTING_ADAPTERS,value.adapter)||typeof value.html!=='boolean'||typeof value.titleClass!=='string'||!/^[-a-zA-Z0-9_]{0,80}$/.test(value.titleClass)||value.titleClass&&!value.html||typeof value.detailPathPrefix!=='string'||value.detailPathPrefix.length>200||value.detailPathPrefix&&(!/^\/[A-Za-z0-9/_-]+\/$/.test(value.detailPathPrefix)||value.detailPathPrefix==='/')||!Array.isArray(value.allowedHosts)||value.allowedHosts.length>3||value.allowedHosts.some(h=>typeof h!=='string'||h!==h.toLowerCase()||!publicHost(h)||new URL('https://'+h).hostname!==h))throw Object.assign(new Error('Проверьте адаптер, HTML-класс заголовка, префикс пути и до3 разрешённых доменов.'),{status:400});
 if(value.pagination!==null&&(!value.pagination||typeof value.pagination!=='object'||Object.keys(value.pagination).some(k=>k!=='queryParam')||typeof value.pagination.queryParam!=='string'||!/^[-a-zA-Z0-9_]{1,40}$/.test(value.pagination.queryParam)))throw Object.assign(new Error('Пагинация: укажите имя query-параметра из реальной ссылки «Следующая». URL не угадываются.'),{status:400});
 return value;
}
function suitability(key,url,companyName){
 if(key==='telegram')return 'Telegram не обрабатывается этим сборщиком.';
 if(!url)return 'Источник не заполнен.';
 let host;try{host=new URL(safeListingUrl(url)).hostname;}catch{return 'URL источника недопустим.';}
 // This verified Sber feed was disabled by 0076. Global source selection must
 // not silently enable it for SberTech again after the active queue drains.
 if(key==='rabota'&&companyName==='СберТех'&&['rabota.ru','www.rabota.ru'].includes(host)&&/^\/company\/sberbank\/vacancies\/?$/.test(new URL(url).pathname))return 'Это источник Сбера, не СберТеха. Укажите отдельный подтверждённый список вакансий СберТеха.';
 // Confirmed corporate pages link to HH but contain no vacancy list of their own.
 const path=new URL(url).pathname;
 if(key==='companySite'&&((companyName==='Bip.ru'&&host==='bip.ru'&&/^\/company\/?$/.test(path))||(companyName==='Rubytech'&&host==='rubytech.ru'&&/^\/career\/?$/.test(path))))return 'На этой странице нет собственного списка вакансий: компания направляет на HH. Используйте отдельный источник HH.';
 if(key==='hh'){try{hhInput(url);return null;}catch{return 'Apify HH: нужна HTTPS-ссылка hh.ru на работодателя, поиск с employer_id или конкретную вакансию.';}}
 if(/(^|\.)(?:hh\.ru|hh\.kz|hh\.uz)$/.test(host))return 'Для hh.ru выберите источник HH с подключением Apify.';
 if(['dreamjob.ru','h.careers'].includes(host))return 'Страница компании/отзывов: требуется подтверждённый список вакансий и отдельный адаптер.';
 if(/(^|\.)(?:t\.me|telegram\.org)$/.test(host))return 'Telegram не обрабатывается этим сборщиком.';
 return null;
}
export async function readVacancyParsingSources(db,{companyId,agencyId,favoritesOnly=false,userName}={}){
 const catalog=await readAppCatalog(db),identities=await ensureCompanyIdentities(db,catalog.companies),agencyIdentities=await ensureAgencyIdentities(db,catalog.agencies);
 const settings=await db.prepare("SELECT coalesce(json_extract(scheduler_json,'$.agenciesEnabled'),0) enabled FROM firecrawl_settings WHERE id=1").first(),agenciesEnabled=settings?.enabled===1;
 const wanted=companyId!=null||agencyId!=null?parserOwner({companyId,agencyId}):null;
 const stored=(await db.prepare('SELECT * FROM vacancy_source_parsing').all()).results||[],byKey=new Map(stored.map(r=>[parserOwnerKey(r)+':'+r.source_id,r]));
 const favorites=favoritesOnly?new Set(((await db.prepare('SELECT company_name FROM company_user_state WHERE user_name=? AND favorite=1').bind(userName??'').all()).results||[]).map(r=>r.company_name)):null;
 const rows=[];
 for(const company of [...catalog.companies.map(c=>({...c,entityType:'company'})),...catalog.agencies.map(c=>({...c,entityType:'agency'}))]){if(favorites&&!favorites.has(company.name))continue;const agency=company.entityType==='agency',id=(agency?agencyIdentities:identities).get(company.name),owner=parserOwner(agency?{agencyId:id}:{companyId:id});if(wanted&&wanted.key!==owner.key)continue;
  for(const [sourceId,sourceName] of VACANCY_SOURCE_META){const url=company.vacancySources?.[sourceId];if(!url)continue;const row=byKey.get(owner.key+':'+sourceId),fresh=row?.source_url===url,reason=agency&&!agenciesEnabled?'Парсинг агентств выключен в настройках Парсера.':suitability(sourceId,url,company.name);let validConfig=true;let config={useEnhancedProxy:false,adapter:(()=>{try{return defaultListingAdapter(url);}catch{return 'generic';}})(),html:false,titleClass:'',detailPathPrefix:'',allowedHosts:[],pagination:null};try{if(fresh)config=validateListingConfig(JSON.parse(row.config_json));}catch{validConfig=false;}
   // Built-in support for the verified Tilda list upgrades untouched generic
   // defaults at read time. Custom title/path rules retain their exact behavior.
   if(!reason&&defaultListingAdapter(url)==='yoomoney'&&config.adapter==='generic'&&!config.titleClass&&!config.detailPathPrefix)config={...config,adapter:'yoomoney',html:true};
   if(config.adapter==='yoomoney')config={...config,html:true};
   rows.push({companyId:owner.companyId,agencyId:owner.agencyId,ownerKey:owner.key,entityType:owner.type,entityId:owner.id,companyName:company.name,sourceId,sourceName,url,supported:!reason,reason,enabled:!!(fresh&&validConfig&&row.enabled&&!reason),method:sourceId==='hh'?'apify':'scrape',config,revision:row?.revision??0,lastSuccessAt:fresh?row.last_success_at:null,lastError:fresh?row.last_error:row?'URL изменился: настройки парсинга нужно сохранить заново.':''});
  }
 }
 return rows;
}
export async function readVacancyParsingSource(db,companyId,sourceId){const owner=parserOwner(companyId),rows=await readVacancyParsingSources(db,{companyId:owner.companyId,agencyId:owner.agencyId});const source=rows.find(r=>r.sourceId===sourceId);if(!source)throw Object.assign(new Error('Существующий источник компании не найден.'),{status:404});return source;}
export async function saveVacancyParsingSource(db,companyId,sourceId,input){
 if(Object.keys(input).some(k=>!['enabled','config','revision','sourceUrl'].includes(k))||typeof input.enabled!=='boolean'||!Number.isSafeInteger(input.revision))throw Object.assign(new Error('Передайте enabled, config, sourceUrl и revision.'),{status:400});
 const owner=parserOwner(companyId),source=await readVacancyParsingSource(db,companyId,sourceId);if(input.sourceUrl!==source.url||source.revision!==input.revision)throw fcError('changed',409);if(!source.supported&&input.enabled)throw Object.assign(new Error(source.reason),{status:422});const config=validateListingConfig(input.config);
 const r=await db.prepare(`INSERT INTO vacancy_source_parsing(${owner.column},source_id,source_url,enabled,config_json,revision) VALUES(?,?,?,?,?,1) ON CONFLICT(${owner.column},source_id) DO UPDATE SET auto_enabled=CASE WHEN vacancy_source_parsing.source_url=excluded.source_url THEN vacancy_source_parsing.auto_enabled ELSE 0 END,next_run_at=CASE WHEN vacancy_source_parsing.source_url=excluded.source_url THEN vacancy_source_parsing.next_run_at ELSE NULL END,source_url=excluded.source_url,enabled=excluded.enabled,config_json=excluded.config_json,revision=vacancy_source_parsing.revision+1,last_error='',last_success_at=NULL WHERE vacancy_source_parsing.revision=?`).bind(owner.id,sourceId,source.url,input.enabled?1:0,JSON.stringify(config),input.revision).run();if(!r.meta?.changes)throw fcError('changed',409);return readVacancyParsingSource(db,companyId,sourceId);
}
