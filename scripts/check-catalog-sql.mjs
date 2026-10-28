// Unchanged source router + actual SQLite :memory:, synthetic rows only.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import router from '../server/routes/index.js';
import {createSession} from '../server/auth/session.js';
import {normalizeCatalog} from '../server/services/catalog.js';
import {DATA} from '../server/data.js';
globalThis.fetch=()=>{throw new Error('Catalog oracle forbids network');};
const secret='CI_catalog_synthetic_session_secret_only_317';
const base=()=>({companies:[
 {name:'CI A',sector:null,roles:17,offices:[{address:'CI address',metro:'CI metro',lat:0,lon:1}],jobs:[{title:' ',url:'https://example.invalid/a'}],custom:{keep:true}},
 {name:'CI B',sector:'IT',jobs:[{title:1,url:'http://example.invalid/b'}]},
 {name:'CI C',sector:'İ ΣΟΣ 😀',jobs:[{title:'CI embedded',url:'https://example.invalid/c'}]}
],agencies:[{name:'CI agency',sector:'agency-only',offices:[{metro:'CI stop'}]}]});
const variants=[['base',JSON.stringify(base())],['fallback shape','{}'],['missing cell',null],['empty','{"companies":[],"agencies":[]}'],['initialized',JSON.stringify(base())],['winner vacancy',JSON.stringify(base())],['winner status',JSON.stringify(base())],['winner updated',JSON.stringify(base())],['winner id',JSON.stringify(base())],['blank status',JSON.stringify(base())],['unknown status',JSON.stringify(base())],['long name',JSON.stringify({companies:[{name:'X'.repeat(4200)}],agencies:[]})]];
let frames=0,responses=0;
for(const [name,raw] of variants){
 const sqlite=new DatabaseSync(':memory:');
 assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
 sqlite.exec('PRAGMA foreign_keys=ON');for(const o of schema.objects)sqlite.exec(o.sql);
 const rows={app_catalog:[],company_identity:[],agency_identity:[],company_crm:[],company_user_state:[],company_vacancies:[],company_applications:[],company_vacancy_state:[]};
 const add=(table,row)=>{rows[table].push(row);const keys=Object.keys(row);sqlite.prepare(`INSERT INTO ${table}(${keys.join(',')}) VALUES(${keys.map(()=>'?').join(',')})`).run(...keys.map(k=>row[k]));};
 if(raw!==null)add('app_catalog',{id:1,data_json:raw});
 const c=normalizeCatalog(raw===null?DATA:JSON.parse(raw));
 c.companies.forEach((v,i)=>add('company_identity',{id:11+i,company_name:v.name}));
 c.agencies.forEach((v,i)=>add('agency_identity',{id:21+i,agency_name:v.name}));
 // All identities are prepared before GET; source bootstrap must change 0 rows.
 if(name!=='long name'&&name!=='empty'){
  for(const company_name of ['CI A','CI B','CI agency'])add('company_crm',{company_name});
  for(const [user,company,favorite,hidden] of [['Eugene','CI A',1,0],['Demo','CI A',0,1],['Nastya','CI B',1,1],['Eugene','CI agency',1,1]])add('company_user_state',{user_name:user,company_name:company,favorite,hidden,comment_md:'CI_PRIVATE_COMMENT'});
  add('company_vacancies',{id:1,company_name:'CI agency',real_company_name:'CI A',url:'https://example.invalid/sql',title:'CI SQL vacancy'});
  add('company_vacancies',{id:2,company_name:'CI A',real_company_name:'CI A',url:'https://example.invalid/once',title:'CI count once'});
  if(name==='initialized')add('company_vacancy_state',{company_name:'CI B'});
  const app=(id,user,status,vacancy_id=null,updated_at='2026-01-01')=>add('company_applications',{id,company_name:'CI A',user_name:user,status,vacancy_id,updated_at,comment_md:'CI_PRIVATE_APPLICATION',application_sources:'[]'});
  app(1,'Eugene','Офер');app(2,'Demo','Отказ');app(3,'Nastya','Скрининг');
  if(name==='winner vacancy')app(4,'Eugene','Отказ',1);
  if(name==='winner status')app(4,'Eugene','Скрининг',null,'2027-01-01');
  if(name==='winner updated'){app(4,'Eugene','CI unknown',1,'2026-02-01');app(5,'Eugene','',2,'2026-01-01');}
  if(name==='winner id'){app(4,'Eugene','CI unknown',1);app(5,'Eugene','',2);}
  if(name==='blank status'){sqlite.exec('DELETE FROM company_applications');rows.company_applications=[];app(4,'Eugene','');}
  if(name==='unknown status'){sqlite.exec('DELETE FROM company_applications');rows.company_applications=[];app(4,'Eugene','CI unknown');}
 }
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this;},async first(column){const r=sqlite.prepare(sql).get(...args)??null;return column&&r?r[column]:r;},async all(){return {results:sqlite.prepare(sql).all(...args)};},async run(){const s=sqlite.prepare(sql);return s.columns().length?{results:s.all(...args)}:{meta:s.run(...args)};}};},async batch(ss){sqlite.exec('BEGIN');try{const r=[];for(const s of ss)r.push(await s.run());sqlite.exec('COMMIT');return r;}catch(e){sqlite.exec('ROLLBACK');throw e;}}};
 const changes=()=>Number(sqlite.prepare('SELECT total_changes() n').get().n);
 const before=changes(),requests=[];
 for(const user of ['Eugene','Demo','Nastya','']){
  const token=user?await createSession(secret,user):null;
  for(const kind of ['companies','agencies']){
   const paths=['list','map','detail?name=CI%20A','detail?name=CI%20agency','11?name=CI%20B','21','00011','9007199254740993?name=CI%20A','9223372036854775807?name=CI%20A','999?name=CI%20A','0?name=CI%20A','detail?name=missing','search?q=undefined','search?q=%C4%B0','search?q=%F0%9F%98%80','search?q=%E2%82','search?q=%zz','search?q=IT;q=undefined','search?q=CI+metro&q=undefined','search?q=undefined&%71=IT','search?q='+encodeURIComponent('x'.repeat(255)+'😀'),'detail?name='+'X'.repeat(4200)];
   for(const path of paths){
    const url='/api/'+kind+'/'+path,method='GET';
    const request=new Request('https://example.invalid'+url,{method,headers:token?{cookie:'it_map_session='+token}:{}});
    const response=await router.fetch(request,{SESSION_SECRET:secret,SITE_PASSWORD:'CI unused',DB:db},{});
    requests.push({user,url,method,status:response.status,body:await response.text()});responses++;
   }
   for(const method of ['POST','PUT','DELETE','HEAD','OPTIONS']){
    const url='/api/'+kind+'/list';
    const response=await router.fetch(new Request('https://example.invalid'+url,{method,headers:token?{cookie:'it_map_session='+token}:{}}),{SESSION_SECRET:secret,SITE_PASSWORD:'CI unused',DB:db},{});
    requests.push({user,url,method,status:response.status,body:await response.text()});responses++;
   }
  }
 }
 assert.equal(changes(),before,'source GET mutated prepared identities/business rows');
 process.stdout.write(JSON.stringify({name,rows,requests})+'\n');frames++;sqlite.close();
}
process.stderr.write(`Catalog actual SQLite/router: ${frames} frames, ${responses} responses, zero GET changes\n`);
