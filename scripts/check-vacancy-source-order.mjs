// Неизменённые source чтения используют полную исходную SQLite-схему и индексы.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {schemaState} from '../server/constants.js';
import {readVacancyProjectionRows} from '../server/services/vacancy-projection.js';
import {readVacancies} from '../server/repositories/vacancies.js';
assert.equal(process.versions.node,'24.19.0');
globalThis.fetch=()=>{throw new Error('Vacancy source order oracle forbids network');};
async function reference(input){
 const sqlite=new DatabaseSync(':memory:');
 try{
  assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
  sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);schemaState.ready=true;
  sqlite.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').run('{"companies":[],"agencies":[]}');
  sqlite.exec("INSERT INTO company_crm(company_name) VALUES('CI GO340 employer'); INSERT INTO company_vacancy_state(company_name) VALUES('CI GO340 employer'); INSERT INTO company_vacancies(id,company_name,url,title) VALUES(1,'CI GO340 employer','https://example.invalid/go340','CI source order')");
  for(const c of input.channels)sqlite.prepare('INSERT INTO telegram_channels(id,name,url,username,normalized_username) VALUES(?,?,?,?,?)').run(c.id,c.name,c.url,c.username,`ci_go340_${c.id}`);
  for(const s of input.sources)sqlite.prepare('INSERT INTO vacancy_telegram_sources(id,vacancy_id,channel_id,message_url,telegram_contact) VALUES(?,1,?,?,?)').run(s.id,s.channel,s.message,s.contact);
  const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async first(c){const r=sqlite.prepare(sql).get(...args)??null;return c&&r?r[c]:r;},async run(){return {meta:sqlite.prepare(sql).run(...args)};}};},async batch(ss){const out=[];for(const s of ss)out.push(await s.run());return out;}};
  const before=sqlite.prepare('SELECT total_changes() n').get().n;
  const expected={list:(await readVacancyProjectionRows(db,'Demo'))[0].telegramSources,detail:(await readVacancyProjectionRows(db,'Demo',1))[0].telegramSources,company:(await readVacancies(db,'Demo','CI GO340 employer'))[0].telegramSources};
  assert.equal(sqlite.prepare('SELECT total_changes() n').get().n,before,'source GET mutated prepared schema');
  return {...input,expected};
 }finally{sqlite.close();}
}
if(process.argv.includes('--reference'))process.stdout.write(JSON.stringify(await reference(JSON.parse(readFileSync(0,'utf8'))))+'\n');
else{
 const frames=[];
 const add=(name,order,patch={})=>{
  const channels=order.map(id=>({id,name:`CI канал ${id} 😀`,url:`https://t.me/ci_go340_${id}`,username:`ci_go340_${id}`}));
  const sources=order.map((channel,i)=>({id:(i+1)*11,channel,message:`https://t.me/ci_go340_${channel}/1`,contact:`CI contact ${channel}`}));
  frames.push({name,channels,sources,...patch});
 };
 add('empty',[]);add('single',[1]);add('reverse',[3,1,2]);add('sparse channel IDs',[400,2,99]);add('source ID order',[9,2,5]);frames.at(-1).sources.reverse();
 add('exact Unicode fields',[8,3,6]);frames.at(-1).channels[0].name='е\u0308\0Я';frames.at(-1).sources[1].contact='CI\0 контакт 😀';
 add('empty optional fields',[3,1,2]);frames.at(-1).channels[0].username='';frames.at(-1).sources[1].contact='';
 add('empty message excluded in full DTO',[3,1,2]);frames.at(-1).sources[0].message='';
 add('unlinked channels',[3,1,2]);frames.at(-1).sources=[];
 let seed=340;const rand=n=>{seed=(Math.imul(seed,1664525)+1013904223)>>>0;return seed%n;};
 for(let i=0;i<48;i++){const order=Array.from({length:1+rand(12)},(_,j)=>(j+1)*7);for(let j=order.length-1;j>0;j--){const k=rand(j+1);[order[j],order[k]]=[order[k],order[j]];}add('random '+i,order);}
 for(const frame of frames)process.stdout.write(JSON.stringify(await reference(frame))+'\n');
 process.stderr.write(`Vacancy Telegram source SQLite: ${frames.length} datasets × 3 views\n`);
}
