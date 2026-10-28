// Actual unchanged frontend/server shared functions; no database or network.
import assert from 'node:assert/strict';
import {publicationDate,publicationBounds,matchesPublicationPeriod} from '../shared/publication-period.ts';
assert.equal(process.versions.node,'24.19.0');
globalThis.fetch=()=>{throw new Error('Publication oracle forbids network');};
let count=0;
const emit=v=>{process.stdout.write(JSON.stringify(v)+'\n');count++;};
const date=raw=>{const v=publicationDate(raw);emit({kind:'date',raw:raw??'',valid:!!v,time:v?.time??0});};
for(let year=0;year<=9999;year++)for(const suffix of ['-01-01','-02-29'])date(String(year).padStart(4,'0')+suffix);
for(const year of [0,1,9,50,99,100,101,400,999,1000,1600,1700,1900,1999,2000,2023,2024,2025,2100,2400,9999])for(let month=0;month<=13;month++)for(const day of [0,1,28,29,30,31,32,99])date(String(year).padStart(4,'0')+'-'+String(month).padStart(2,'0')+'-'+String(day).padStart(2,'0'));
const whitespace=[...Array.from({length:33},(_,i)=>String.fromCharCode(i)),...['\u0085','\u00a0','\u1680','\u180e','\u2000','\u2001','\u2002','\u2003','\u2004','\u2005','\u2006','\u2007','\u2008','\u2009','\u200a','\u200b','\u2028','\u2029','\u202f','\u205f','\u3000','\ufeff']];
for(const w of whitespace)for(const tail of ['',' 00:00:00','Tinvalid suffix']) {date(w+'2024-02-29'+tail+w);date('2024-02-29'+w+tail);}
for(const raw of [null,undefined,'','bad','2024-2-29','2024-02-9','２０２４-02-29','2024/02/29','+002024-02-29','10000-01-01','\ud8002024-02-29','2024-02-29\ud800','2024-02-290extra'])date(raw);
const nows=['2024-03-31T12:00:00Z','2023-03-31T12:00:00Z','2024-04-30T12:00:00Z','2024-01-31T12:00:00Z','2024-02-29T12:00:00Z','2026-10-28T20:59:59.999Z','2026-10-28T21:00:00Z','2026-10-28T21:00:00.001Z','2010-03-27T23:30:00Z','2014-10-25T22:30:00Z','2000-03-31T00:00:00Z','2100-03-31T00:00:00Z','2400-03-31T00:00:00Z'].map(Date.parse);
for(const now of nows) {
 const points=new Set();
 for(const p of ['week','month','twoMonths']){const b=publicationBounds(p,now);for(const edge of [b.from,b.to])for(const offset of [-86400000,0,86400000])points.add(new Date(edge+offset).toISOString().slice(0,10));}
 for(const period of ['week','month','twoMonths','archive','all'])for(const raw of [...points,'','invalid','0001-01-01','0099-12-31','\ufeff2024-02-29','\u00852024-02-29'])for(const dates of [{publishedAt:raw},{publishedAt:'bad',loadedAt:raw},{publishedAt:'',loadedAt:'bad',createdAt:raw},{publishedAt:raw,loadedAt:'1900-01-01',createdAt:'1900-01-01'}])emit({kind:'match',now,period,dates,want:matchesPublicationPeriod(dates,period,now)});
}
process.stderr.write(`Publication unchanged source: ${count} date/filter frames\n`);
