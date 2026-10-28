// Реальное SUM(json_extract(...)) в SQLite, включая типы и переполнение.
import {DatabaseSync} from 'node:sqlite';
import assert from 'node:assert/strict';
const sqlite=new DatabaseSync(':memory:');
assert.equal(sqlite.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
sqlite.exec('CREATE TABLE input(payload TEXT)');
const scalar=['null','false','true','0','-0','1','1.0','1e0','1e-999','1e400','-1e400','9007199254740991','9223372036854775807','-9223372036854775808','9223372036854775808','[]','{}','"3"','"3.0"','"3tail"','"0x10"','"+ .1"','"1e+"','"1e2tail"','"1\\u0000tail"','"\\ufeff3"','" 3 "','""','"+.1"','"-.1"','".5e2"','"9223372036854775807"','"-9223372036854775808"','"9223372036854775808"','"1e999"','"-1e999"'];
const frames=[[],['9223372036854775807','1'],['-9223372036854775808','-1'],['9223372036854775807','1','0.0'],['9223372036854775807','1','null'],['1.55e308','1.23','3.2e-16','-1.55e308','-1.23'],['1e400','-1e400'],['not JSON']];
for(const raw of scalar)for(const prefix of [[],['1'],['0.0'],['9223372036854775807'],['9223372036854775807','1']])frames.push([...prefix,raw]);
for(const raw of scalar)frames.push([raw,raw,raw],[raw,'0.0',raw]);
const sum=sqlite.prepare("SELECT coalesce(sum(json_extract(payload,'$.found')),0) s FROM input");sum.setReadBigInts(true);
for(const values of frames){
 sqlite.exec('DELETE FROM input');for(const v of values)sqlite.prepare('INSERT INTO input VALUES(?)').run('{"found":'+v+'}');
 let body='',status='ok';try{let n=sum.get().s;if(typeof n==='bigint')n=Number(n);body=JSON.stringify(n);}catch{status='storage';}
 process.stdout.write(JSON.stringify({values,body,status})+'\n');
}
process.stderr.write(`Суммы dashboard: ${frames.length} последовательностей SQLite\n`);
