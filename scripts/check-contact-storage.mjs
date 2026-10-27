// Unchanged contact repositories on synthetic SQLite :memory:, never file/network.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
import {BACKUP_SCHEMA} from '../server/data.js';
import {schemaState} from '../server/constants.js';
import {readHiringContacts,createHiringContact,updateHiringContact,deleteHiringContact} from '../server/repositories/contacts.js';
import {readCompanyNames} from '../server/repositories/catalog.js';
globalThis.fetch=()=>{throw new Error('Contact oracle forbids network');};schemaState.ready=true;
const sqlite=new DatabaseSync(':memory:');sqlite.exec('PRAGMA foreign_keys=ON');for(const o of BACKUP_SCHEMA.objects)sqlite.exec(o.sql);
sqlite.function('current_timestamp',()=> '2026-10-27 08:00:00');
const db={prepare(sql){let args=[];return {bind(...v){args=v;return this},async first(){return sqlite.prepare(sql).get(...args)||null},async all(){return {results:sqlite.prepare(sql).all(...args)}},async run(){const r=sqlite.prepare(sql).run(...args);return {meta:{last_row_id:Number(r.lastInsertRowid),changes:Number(r.changes)}}}}}};
const company='CI Contact Employer',agency='CI Contact Agency',catalog={companies:[{name:company}],agencies:[{name:agency}]};sqlite.prepare('INSERT INTO app_catalog(id,data_json)VALUES(1,?)').run(JSON.stringify(catalog));
const steps=[];async function run(name,method,id,payload,want,owner=company){let result=null,error='',status=method==='POST'?201:200;try{
 if(method==='GET'||method==='POST'){const names=await readCompanyNames(db);owner=method==='POST'?String(payload?.companyName||'').trim():owner;if(!owner||!names.has(owner)){status=404;error='Компания или агентство не найдены.'}else result=method==='GET'?{companyName:owner,limit:3,contacts:await readHiringContacts(db,owner)}:await createHiringContact(db,owner,payload)}else{result=method==='PUT'?await updateHiringContact(db,id,payload):await deleteHiringContact(db,id);if(result===null){status=404;error='Контакт не найден.'}}
 }catch(e){status=400;error=e.message}assert.equal(status,want,name);steps.push({name,method,id,payload,owner,status,result,error});return result}
const one=await run('create rich contact','POST',null,{companyName:company,position:1,fullName:' CI First ',linkedinUrl:'https://example.invalid/ci',telegram:' @ci_first ',phone:' +0 000 CI ',email:'ci@example.invalid',specialization:' CI work '},201);const first=one.contacts[0].id;
await run('duplicate position','POST',null,{companyName:company,position:1,fullName:'CI duplicate'},400);
await run('PUT replaces other fields while position falls back','PUT',first,{fullName:' CI Changed '},200);
await run('null position retains current','PUT',first,{position:null,fullName:'CI Retained'},200);
await run('PUT requires full name','PUT',first,{phone:'CI only phone'},400);
await run('create position three','POST',null,{companyName:company,position:3,fullName:'CI Third'},201);
const three=(await readHiringContacts(db,company)).find(c=>c.position===3).id;
await run('create position two','POST',null,{companyName:company,position:'2',fullName:'CI Second'},201);
await run('capacity error precedes duplicate','POST',null,{companyName:company,position:2,fullName:'CI over limit'},400);
await run('PUT cannot occupy used position','PUT',first,{position:2,fullName:'CI conflict'},400);
await run('unknown company checked before invalid fields','POST',null,{companyName:'CI unknown',position:0},404);
await run('owner spoof ignored on PUT','PUT',first,{companyName:agency,fullName:'CI Stable owner'},200);
await run('agency has independent slots','POST',null,{companyName:agency,position:1,fullName:'CI Agency'},201);
await run('read company ordered contacts','GET',null,null,200);
await run('read agency isolation','GET',null,null,200,agency);
await run('unknown GET','GET',null,null,404,'CI unknown');
await run('delete frees position','DELETE',three,null,200);
await run('move to freed position','PUT',first,{position:3,fullName:'CI Moved'},200);
await run('missing update skips field validation','PUT',99999,{},404);
await run('missing delete','DELETE',99999,null,404);
await run('delete existing contact','DELETE',first,null,200);
await run('read remaining company','GET',null,null,200);
assert.equal(steps.length,21);assert.equal(sqlite.prepare('SELECT COUNT(*) n FROM hiring_contacts').get().n,2);
if(process.argv.includes('--stdio'))process.stdout.write(JSON.stringify({catalog,company,agency,steps}));else console.log(`PASS: ${steps.length} unchanged contact storage operations, synthetic SQLite :memory: only`);sqlite.close();
