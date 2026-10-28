// Unchanged source readVacancyRoles with an explicit preselected read set.
// This is not SQL selection, schema initialization or database acceptance.
import assert from 'node:assert/strict';
import {readVacancyRoles} from '../server/repositories/vacancy-roles.js';
assert.equal(process.versions.node,'24.19.0');assert.equal(process.versions.icu,'78.3');
globalThis.fetch=()=>{throw new Error('Role oracle forbids network');};
const role=(id,name,extra={})=>({id,stableKey:'CI_role_'+id,name,parentId:null,sortOrder:0,usageCount:0,childCount:0,...extra});
const fixtures=[['empty',[]],['one',[role(1,'CI role')]],['source counts',[role(1,'CI source count',{childCount:7,usageCount:9})]],['metadata and flat order',[role(4,'Child',{parentId:2}),role(1,'Root',{childCount:1}),role(3,'Other',{usageCount:8}),role(2,'Middle',{parentId:1,childCount:1})]]];
const names=['ИТ','А','а','Ё','Е','É','E\u0301','e','A','İ','ß','Σ','A10','A2','_','-','ﬁ','Å','A\u030a','😀','Я','я','Ёлка','Елка','Б','б','А','A'];
for(let i=0;i<128;i++){
 const rows=[role(1,'CI root',{childCount:names.length})];
 for(let j=0;j<names.length;j++){const n=(j+i)%names.length;rows.push(role(j+2,names[n],{parentId:1,sortOrder:i%4===0?0:(j*7+i)%5-2,usageCount:(j+i)%9}));}
 if(i%2)rows.reverse();fixtures.push(['siblings '+i,rows]);
}
for(let i=0;i<32;i++){
 const rows=[];
 for(let j=1;j<=32;j++)rows.push(role(j,names[(j+i)%names.length],{parentId:j<=4?null:Math.floor((j-1)/2),sortOrder:(j+i)%4-2,usageCount:j%7}));
 for(const r of rows)r.childCount=rows.filter(c=>c.parentId===r.id).length;
 if(i%2)rows.reverse();fixtures.push(['forest '+i,rows]);
}
let count=0;
for(const [name,rows] of fixtures){
 let queries=0;
 const db={prepare(sql){assert.ok(sql.includes('FROM vacancy_roles role')&&sql.includes('COUNT(DISTINCT assignment.vacancy_id)')&&sql.includes('ORDER BY role.parent_id IS NOT NULL'));queries++;return {async all(){return {results:structuredClone(rows)};}};}};
 const body=await readVacancyRoles(db);assert.equal(queries,1);
 process.stdout.write(JSON.stringify({name,rows,body})+'\n');count++;
}
process.stderr.write(`Role catalog unchanged source: ${count} read-set frames; no SQL/database acceptance\n`);
