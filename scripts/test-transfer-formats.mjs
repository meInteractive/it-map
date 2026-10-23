import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import { DatabaseSync } from 'node:sqlite';

// Exercise the real HTTP handlers against SQLite, including fresh Worker instances.
const sqlite = new DatabaseSync(':memory:');
sqlite.exec('PRAGMA foreign_keys = ON');
const db = {
  prepare(sql) {
    let values = [];
    return {
      bind(...args) { values = args; return this; },
      async all() { return {results:sqlite.prepare(sql).all(...values)}; },
      async first() { return sqlite.prepare(sql).get(...values) ?? null; },
      async run() {
        const prepared = sqlite.prepare(sql);
        if (prepared.columns().length) return {success:true, results:prepared.all(...values)};
        const result = prepared.run(...values);
        return {success:true,meta:{changes:Number(result.changes),last_row_id:Number(result.lastInsertRowid)}};
      }
    };
  },
  async batch(statements) {
    sqlite.exec('BEGIN');
    try {
      const results = [];
      for (const statement of statements) results.push(await statement.run());
      sqlite.exec('COMMIT');
      return results;
    } catch (error) { sqlite.exec('ROLLBACK'); throw error; }
  }
};
const env = {DB:db,SITE_PASSWORD:'test-only',NASTYA_PASSWORD:'test-only',SESSION_SECRET:'test-only-secret'};
const worker = (await import(new URL('../.tests/backend.mjs?transfer-test', import.meta.url))).default;
const login = await worker.fetch(new Request('https://test.example/auth', {method:'POST',body:new URLSearchParams({login:'Eugene',password:'test-only'})}), env);
assert.equal(login.status,303);
const cookie=login.headers.get('set-cookie').split(';')[0];
const request = (path, payload) => worker.fetch(new Request('https://test.example'+path, {method:payload?'POST':'GET',headers:{cookie,'content-type':'application/json'},body:payload?JSON.stringify(payload):undefined}),env);
const backup = async () => {const response=await request('/api/backup');assert.equal(response.status,200);return response.json();};
const restore = async data => {const response=await request('/api/backup',data);const body=await response.json();assert.equal(response.status,200,JSON.stringify(body));};
const initial=await backup();
assert.equal(initial.version,39);
// Exercise legacy conversion explicitly; v16/v17 files are immutable snapshots.
const legacy = data => ({...data, version:15, storage:{tables:data.storage.tables.filter(t => !['parser_error_daily','parser_candidate_intakes','apify_settings','vacancy_role_settings','vacancy_role_results','vacancy_parser_fields','vacancy_parser_roles','vacancy_publication_sources','vacancy_publication_keys','vacancy_import_operations','vacancy_detail_settings','vacancy_raw_snapshots','vacancy_raw_snapshot_parts','vacancy_content_fetches','vacancy_extractions','app_catalog','google_calendar_connection','user_profiles','openrouter_settings','openrouter_ai_logs','openrouter_model_metrics','system_prompts','vacancy_cover_letters','prompt_model_settings','openai_settings','vacancy_roles','vacancy_role_assignments','vacancy_role_classification','vacancy_candidates','vacancy_candidate_identities','vacancy_classification_attempts','vacancy_classifications','firecrawl_settings','vacancy_source_parsing'].includes(t.name))}});
Object.assign(initial,legacy(initial));
initial.catalog.companies=initial.catalog.companies.slice(0,1);
initial.catalog.agencies=[];
const company=initial.catalog.companies[0];
Object.assign(company,{aiScore:10,aiEvidence:'ИИ-продукт: https://example.test/ai',wlbScore:37,ratingFormulaVersion:5});
company.vacancySources={...company.vacancySources,dreamJob:'https://dreamjob.ru/employers/example/vakansii',hCareers:'https://h.careers/companies/example'};
await restore(initial);
const saved=await backup();
const savedCompany=saved.catalog.companies[0];
assert.equal(savedCompany.aiScore,10);
assert.equal(savedCompany.aiEvidence,company.aiEvidence);
assert.equal(savedCompany.wlbScore,37);
assert.equal(savedCompany.vacancySources.dreamJob,company.vacancySources.dreamJob);
assert.equal(savedCompany.vacancySources.hCareers,company.vacancySources.hCareers);
await restore(saved);
assert.deepEqual((await backup()).catalog,saved.catalog);
// A rejected backup must not change the stored catalog.
for(const patch of [{aiScore:7},{aiScore:'5'},{aiScore:5,aiEvidence:''},{aiEvidence:42},{aiEvidence:'x'.repeat(10001)}]) {
  const invalid=legacy(structuredClone(saved));Object.assign(invalid.catalog.companies[0],patch);
  assert.equal((await request('/api/backup',invalid)).status,400);
  assert.deepEqual((await backup()).catalog,saved.catalog);
}
const missing=legacy(structuredClone(saved));delete missing.catalog.companies[0].aiScore;
assert.equal((await request('/api/backup',missing)).status,400);
// Version 14 can contain AI fields from the previous release, or omit them entirely.
const old=legacy(structuredClone(saved));old.version=14;
await restore(old);assert.equal((await backup()).catalog.companies[0].aiScore,10);
delete old.catalog.companies[0].aiScore;delete old.catalog.companies[0].aiEvidence;
old.catalog.companies[0].ratingFormulaVersion=4;
await restore(old);
const migrated=(await backup()).catalog.companies[0];
assert.equal(migrated.aiScore,null);assert.equal(migrated.aiEvidence,'');
assert.equal(migrated.wlbScore,37);assert.equal(migrated.score,savedCompany.score-10);
// Import new fields; an old spreadsheet update must preserve the existing AI assessment.
const update = async row => {
 const response=await request('/api/companies/import',{mode:'update',companies:[{name:company.name,...row}],offices:[],reviews:[],agencies:[],contacts:[]});
 const body=await response.json();assert.equal(response.status,200,JSON.stringify(body));assert.deepEqual(body.errors,[]);
};
await update({aiScore:'5',aiEvidence:company.aiEvidence});
await update({wlbScore:37});
assert.equal((await backup()).catalog.companies[0].aiScore,5);
await update({vacancyDreamJob:'https://dreamjob.ru/employers/updated/vakansii',hCareers:'https://h.careers/companies/updated',rabota:'https://rabota.ru/company/updated/vacancies'});
const updatedSources=(await backup()).catalog.companies[0].vacancySources;
assert.equal(updatedSources.dreamJob,'https://dreamjob.ru/employers/updated/vakansii');
assert.equal(updatedSources.hCareers,'https://h.careers/companies/updated');
assert.equal(updatedSources.rabota,'https://rabota.ru/company/updated/vacancies');
await update({wlbScore:37});assert.equal((await backup()).catalog.companies[0].vacancySources.rabota,updatedSources.rabota,'Old workbook update preserves rabota');
const invalidSource=await request('/api/companies/import',{mode:'update',companies:[{name:company.name,rabota:'javascript:alert(1)'}],offices:[],reviews:[],agencies:[],contacts:[]});
assert.equal((await invalidSource.json()).invalid,1);assert.equal((await backup()).catalog.companies[0].vacancySources.rabota,updatedSources.rabota);
// A full exported workbook can be re-imported in create mode: existing rows are
// skipped before strict validation, while a new row keeps descriptive fields,
// multiple INNs, its office, review sources and a contact linked by name.
const importedName='S.AI Import Compatibility';
const createResponse=await request('/api/companies/import',{
 mode:'create',
 companies:[
  {row:2,name:company.name,deferment:'This deliberately invalid duplicate must be skipped',aiScore:74},
  {row:3,companyId:9001,name:importedName,sector:'AI',roles:'Head of AI',rating:4.5,wlb:'Стабильный график',wlbScore:80,
   deferment:'Работа в аккредитованной IT-компании; применимость отсрочки зависит от оформления',mobilization:'Публично не подтверждено — уточнить у HR',confidence:'Высокая по вакансиям; средняя по условиям',
   housingStatus:'Льгота зависит от юридического лица',revenueInn:'7707083893; 7730253720',revenueStatus:'Подтверждено по двум юридическим лицам',
   vpn:'Разрешён только корпоративный VPN',ai:'ИИ — основной профиль подразделения',chatgpt:'Не подтверждено',claude:'Доступ зависит от команды',grok:'Не подтверждено',qwen:'Доступен во внутреннем контуре',deepseek:'Доступен во внутреннем контуре',vpnAiEvidence:'Подтверждено публичными вакансиями',
   aiScore:10,aiEvidence:'AI-направление: https://example.test/ai',vacancyDreamJob:'https://dreamjob.ru/employers/import-test/vakansii',hCareers:'https://h.careers/companies/import-test',rabota:'https://rabota.ru/company/import-test/vacancies'}
 ],
 offices:[{row:2,companyId:9001,companyName:importedName,role:'Основной офис',address:'Москва, Тестовая улица, 1',metro:'Тестовая',main:'Да',status:'Офис команды подтверждён публичным источником',source:'https://example.test/office',lat:'',lon:''}],
 reviews:[{row:2,companyId:9001,reviewDreamJob:'https://dreamjob.ru/employers/import-test'}],
 agencies:[],
 contacts:[{row:2,entityType:'Компания',entityId:9001,entityName:importedName,position:1,fullName:'Тестовый Контакт',email:'test@example.test'}]
});
const createBody=await createResponse.json();
assert.equal(createResponse.status,200,JSON.stringify(createBody));
assert.equal(createBody.added,1);assert.equal(createBody.duplicates,1);assert.equal(createBody.invalid,0);assert.equal(createBody.officesAdded,1);assert.equal(createBody.reviewsUpdated,1);assert.equal(createBody.contactsAdded,1);assert.deepEqual(createBody.errors,[]);
const imported=(await backup()).catalog.companies.find(item=>item.name===importedName);
assert.ok(imported);assert.equal(imported.vacancySources.rabota,'https://rabota.ru/company/import-test/vacancies');assert.equal(imported.deferment,'Работа в аккредитованной IT-компании; применимость отсрочки зависит от оформления');
assert.equal(imported.revenue.inn,'7707083893; 7730253720');assert.equal(imported.vacancySources.dreamJob,'https://dreamjob.ru/employers/import-test/vakansii');assert.equal(imported.reviewSources.dreamJob,'https://dreamjob.ru/employers/import-test');
// Inspect the actual generated XLSX archive, including typed numeric cells and its instructions.
const xlsx=await request('/api/exports/companies');assert.equal(xlsx.status,200);
const bytes=Buffer.from(await xlsx.arrayBuffer()), entries=new Map();
for(let offset=0;bytes.readUInt32LE(offset)===0x04034b50;){
 assert.equal(bytes.readUInt16LE(offset+8),0,'Fixture expects the application’s stored ZIP encoding');
 const size=bytes.readUInt32LE(offset+18),nameLength=bytes.readUInt16LE(offset+26),extraLength=bytes.readUInt16LE(offset+28);
 const start=offset+30+nameLength+extraLength;
 entries.set(bytes.subarray(offset+30,offset+30+nameLength).toString(),bytes.subarray(start,start+size).toString());offset=start+size;
}
const sheet=entries.get('xl/worksheets/sheet1.xml');
assert.ok(sheet.includes('Балл ИИ'));assert.ok(sheet.includes('ИИ — обоснование и источники'));
assert.ok(sheet.includes('DreamJob'));assert.ok(sheet.includes('Эйч'));assert.ok(sheet.includes('rabota.ru'));assert.ok(sheet.includes(updatedSources.rabota));
const scoreColumn=sheet.match(/<c r="([A-Z]+)1"[^>]*><is><t[^>]*>Балл ИИ<\/t>/)[1];
assert.match(sheet,new RegExp('<c r="'+scoreColumn+'2"[^>]*><v>5</v>'));
assert.ok(sheet.includes(company.aiEvidence));
assert.ok(sheet.includes(updatedSources.dreamJob));assert.ok(sheet.includes(updatedSources.hCareers));
const instructions=entries.get('xl/worksheets/sheet6.xml');
assert.ok(instructions.includes('Формула рейтинга v5'));
assert.ok(instructions.includes('Старые файлы без колонок ИИ поддерживаются'));
await update({aiScore:0});assert.equal((await backup()).catalog.companies[0].aiScore,0);
const vacancySourcesXlsx=await request('/api/exports/companies-vacancies');assert.equal(vacancySourcesXlsx.status,200);
const vacancySourcesBytes=Buffer.from(await vacancySourcesXlsx.arrayBuffer());
assert.ok(vacancySourcesBytes.includes(Buffer.from('DreamJob')));
assert.ok(vacancySourcesBytes.includes(Buffer.from('Эйч')));assert.ok(vacancySourcesBytes.includes(Buffer.from('rabota.ru')));assert.ok(vacancySourcesBytes.includes(Buffer.from(updatedSources.rabota)));
assert.ok(vacancySourcesBytes.includes(Buffer.from('autoFilter ref="A1:U')));
const {EntitySources}=await moduleFor('frontend/features/companies/CompanyDrawer.tsx');
const html=render(EntitySources,{value:{rabota:updatedSources.rabota}});assert.ok(html.includes(updatedSources.rabota));assert.ok(html.includes('>rabota.ru<'));
const {normalizeVacancySources}=await moduleFor('server/services/catalog.js');assert.equal(normalizeVacancySources({name:'Synthetic source',vacancySources:{rabota:updatedSources.rabota}}).rabota,updatedSources.rabota);
sqlite.close();
console.log('Transfer formats verified: backup v25 round-trip, compatible full-workbook company import, DreamJob/Эйч/rabota.ru fields and legacy imports');
