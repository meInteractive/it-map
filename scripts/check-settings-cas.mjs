// SQLite JSON1 comparisons only, no schema/data/provider access.
import assert from 'node:assert/strict';
import {DatabaseSync} from 'node:sqlite';
const db=new DatabaseSync(':memory:');assert.equal(db.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
const values=['null','true','false','"7"','[]','[7]','{}','{"toString":null}','0','-0','1','-1','7','7.0','7e0','7.0000000000000001','7.0000000000000009','1e-999','1e999','-1e999','9007199254740991','9007199254740991.1','9007199254740992','-9007199254740991','9223372036854775807','9223372036854775808'];
for(let i=0;i<300;i++)values.push((i%2?'-':'')+(i*982451653+1)+'.'+String(i*71).padStart(6,'0')+'e'+(i%23-11));
const raw=['{}','[]','null','{"\\u0072evision":7}','{"revision":6,"revision":7}','{"revision":false,"revision":7}',...values.map(v=>' { "revision" : '+v+' } ')];
const query=db.prepare("SELECT json_extract(?,'$.revision') = ? AS yes");let count=0;
for(const text of raw)for(const expected of [-9007199254740991,-1,0,1,6,7,9007199254740991]){process.stdout.write(JSON.stringify({raw:text,expected,match:query.get(text,expected).yes===1})+'\n');count++;}
db.close();process.stderr.write(`Settings JSON1 CAS: ${count} numeric/first-key comparisons\n`);
