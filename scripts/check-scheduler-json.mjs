// JSON1 extraction/SQL text used by the unchanged source HH page eligibility.
// All data are synthetic and SQLite :memory:, no providers/files/network.
import {DatabaseSync} from 'node:sqlite';
import assert from 'node:assert/strict';
const db=new DatabaseSync(':memory:');assert.equal(db.prepare('SELECT sqlite_version() v').get().v,'3.53.3');
globalThis.fetch=()=>{throw Error('Scheduler JSON oracle forbids network');};
const query=db.prepare("SELECT json_extract(?,?) IS NULL empty,hex(CAST(json_extract(?,?) AS TEXT)) text");
let count=0;
function vector(raw,path='$.runId') {const result=query.get(raw,path,raw,path);process.stdout.write(JSON.stringify({raw,path,empty:!!result.empty,hex:result.text})+'\n');count++;}
for(const value of ['null','true','false','0','-0','1','1.0','1e0','1e21','1e17','1e16','1e-4','1e-5','-0.0','1e400','-1e400','1e-400','-1e-400','9223372036854775807','-9223372036854775808','9223372036854775808','1.2345678901234567','3500000000000000.2500001','3500000000000000.2499999','"CI"','"CI\\ud800"','"CI\\udfff"','"CI\\ud83d\\ude00"','"\\u0000CI\\n"','[1, 2]','{"x": 1.00, "y":"\\u0041"}'])vector('{"runId":'+value+'}');
for(const raw of ['{}','[]','null','"CI"','{"runId":null,"runId":"CI second"}','{"runId":"CI first","runId":3}','{"RunId":"CI wrong case"}','{"\\u0072unId":"CI escaped key"}'])vector(raw);
for(const raw of ['{}','{"paused":{"apify":{"page":0}}}','{"paused":{"apify":{"page":1.0}}}','{"paused":{"apify":{"page":null}}}','{"paused":{"apify":{"page":2}},"paused":{"apify":{"page":9}}}'])vector(raw,'$.paused.apify.page');
let state=0x814dec03;const random=()=>{state^=state<<13;state^=state>>>17;state^=state<<5;return state>>>0};
for(let i=0;i<3000;i++) {const value=(random()-0x80000000)*10**(random()%45-22)+(random()/2**32);vector(JSON.stringify({runId:value}));}
const bits=new DataView(new ArrayBuffer(8));let extra=0;
while(extra<20000){bits.setUint32(0,random());bits.setUint32(4,random());const value=bits.getFloat64(0);if(!Number.isFinite(value))continue;vector(JSON.stringify({runId:value}));extra++;}
assert.equal(count,23044);db.close();
