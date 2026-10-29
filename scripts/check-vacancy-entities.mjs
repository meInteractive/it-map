// Неизменённый readEntityDirectory читает синтетический app_catalog на SQLite.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {DatabaseSync} from 'node:sqlite';
import {readEntityDirectory} from '../server/services/catalog-projection.js';
assert.equal(process.versions.node,'24.19.0');assert.equal(process.versions.icu,'78.3');
globalThis.fetch=()=>{throw new Error('Vacancy entity oracle forbids network');};
async function reference(input){
 const sqlite=new DatabaseSync(':memory:');
 try{
  sqlite.exec('CREATE TABLE app_catalog(id INTEGER PRIMARY KEY,data_json TEXT)');
  if(input.raw!==null)sqlite.prepare('INSERT INTO app_catalog VALUES(1,?)').run(input.raw);
  const db={prepare(sql){let values=[];return {bind(...v){values=v;return this;},async first(){return sqlite.prepare(sql).get(...values)??null;}};}};
  try{return {...input,expected:await readEntityDirectory(db,input.names,true),error:false};}
  catch{return {...input,expected:null,error:true};}
 }finally{sqlite.close();}
}
if(process.argv.includes('--reference'))process.stdout.write(JSON.stringify(await reference(JSON.parse(readFileSync(0,'utf8')))));
else{
 const base=()=>({companies:[{name:'CI B',rank:90,jobs:[],offices:[]},{name:'CI A',rank:80,jobs:[],offices:[]}],agencies:[{name:'CI agency',rank:9}]});
 const variants=[['defaults',JSON.stringify(base())],['missing',null],['empty raw',''],['invalid JSON','{'],['null JSON','null'],['missing agencies',JSON.stringify({companies:[{name:'CI A'}]})],['empty companies',JSON.stringify({companies:[],agencies:[]})]];
 for(const value of [null,false,0,1,'100',['100','200'],[300,100],['5e2',null],{from:100,to:200}]){
  const c=base();for(const entity of [...c.companies,...c.agencies]){entity.middle=value;entity.senior=value;entity.lead=value;entity.head=value;entity.applicationUrl=typeof value==='string'?' \uFEFFhttps://example.invalid/apply?x=<private> \t':value;entity.resume='CI_PRIVATE_MUST_NOT_APPEAR';}
  variants.push(['fields '+JSON.stringify(value),JSON.stringify(c)]);
 }
 const scored=base();scored.companies[0].ratingInputs={roleFit:10};scored.companies[1].ratingInputs={roleFit:0};variants.push(['global ranks',JSON.stringify(scored)]);
 const unicode=base();unicode.companies=[{name:'Å'},{name:'A\u030a'},{name:'А'},{name:'A'},{name:'😀'}];variants.push(['exact names',JSON.stringify(unicode)]);
 const names=[[],['CI A'],['CI agency'],['CI A','CI agency'],['CI B','CI A'],['CI missing','CI A'],['CI A','CI A'],['Å','A\u030a'],['А','A'],['😀','CI missing']];
 let count=0;for(const [name,raw]of variants)for(const selected of names){process.stdout.write(JSON.stringify(await reference({name:name+'/'+JSON.stringify(selected),raw,names:selected}))+'\n');count++;}
 console.error('Vacancy entity source SQLite: '+count+' frames');
}
