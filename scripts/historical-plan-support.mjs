// Metadata-only source planner spies; never executes SQL.
import {schema} from './historical-format-support.mjs';
const definitions=[...schema.tables,...schema.operationalTables];
export const emptyCurrent=()=>schema.tables.map(t=>({name:t.name,columns:t.columns,rows:[]}));
export function currentRow(current,name,values){const t=current.find(t=>t.name===name),r=Object.fromEntries(t.columns.map(c=>[c.name,null]));Object.assign(r,values);t.rows.push(r);return r;}
export function spy(current){
 const actual=definitions.filter(d=>current.some(t=>t.name===d.name)||schema.operationalTables.some(t=>t.name===d.name));
 const db={batches:[],prepare(sql){let args=[];return {sql,get args(){return args;},bind(...a){args=a;return this;},async all(){
  if(/FROM sqlite_schema/.test(sql))return {results:actual.map(t=>({name:t.name}))};
  const name=sql.match(/PRAGMA (?:table_info|foreign_key_list)\("([^"]+)"\)/)?.[1],t=actual.find(t=>t.name===name);
  if(t&&sql.startsWith('PRAGMA table_info'))return {results:t.columns.map(c=>({name:c.name,type:c.type,notnull:Number(c.notNull),pk:Number(c.primaryKey),dflt_value:c.defaultValue}))};
  if(t&&sql.startsWith('PRAGMA foreign_key_list'))return {results:t.foreignKeys};
  const selected=sql.match(/^SELECT (?:\*|id,stable_key) FROM (?:"([^"]+)"|([a-z_]+))$/);
  if(selected)return {results:structuredClone(current.find(t=>t.name===(selected[1]||selected[2]))?.rows||[])};
  throw new Error('Unexpected historical source query');
 },async first(){throw new Error('Unexpected historical first');},async run(){throw new Error('Historical oracle forbids SQL');}};},async batch(statements){db.batches.push(statements);return [];}};return db;
}
