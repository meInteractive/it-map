// Actual unchanged bounded projection, explicit selected read sets; no SQL.
import {schedulerAttemptDetails} from '../server/services/scheduler-attempt-details.js';
globalThis.fetch=()=>{throw new Error('Attempt projection oracle forbids network');};
const results=['','{','null','true','1','"CI text"','[]','{}',
 '{"accepted":true}','{"accepted":false}','{"accepted":1}','{"accepted":"true"}',
 '{"roleIds":[]}','{"roleIds":[1,2]}','{"roleIds":[1,1]}','{"roleIds":[0]}','{"roleIds":[-0]}','{"roleIds":[-1]}',
 '{"roleIds":[1.1]}','{"roleIds":["1"]}','{"roleIds":[true]}','{"roleIds":[null]}','{"roleIds":{}}',
 '{"roleIds":[9007199254740991]}','{"roleIds":[9007199254740992]}','{"roleIds":[1e400]}',
 '{"roleIds":[1.0000000000000001]}','{"roleIds":[1e0,1.0]}',
 '{"accepted":true,"accepted":false}','{"roleIds":[1],"roleIds":[2]}',
 JSON.stringify({roleIds:Array.from({length:12},(_,i)=>i+1)}),JSON.stringify({roleIds:Array.from({length:13},(_,i)=>i+1)}),
 '{"superseded":true,"nextStage":null}','{"superseded":true}','{"superseded":1,"nextStage":null}',
 '{"superseded":true,"nextStage":"diagnostic"}','{"superseded":true,"nextStage":false}',
 '{"accepted":true,"prompt":"CI_PRIVATE_PROMPT","provider":{"key":"CI_SYNTHETIC_PRIVATE"}}',
 '{"accepted":true,"private":"\\ud800"}',
 JSON.stringify({accepted:true,private:'CI_PRIVATE_TEXT'.repeat(300)}).slice(0,2048)];
for(const nextStage of ['classify_title','fetch_full_text','extract_vacancy','assign_roles','import_vacancy','score_relevance'])results.push(JSON.stringify({superseded:true,nextStage}));
const types=['classify_title','assign_roles','import_vacancy','scan_source','fetch_full_text','extract_vacancy','score_relevance','diagnostic'];
let count=0;
for(const type of types)for(const result_json of results){
 const row={id:'CI_attempt',job_id:'CI_job',run_id:'CI_run',type,company_id:1,agency_id:null,source_id:'hh',candidate_id:null,started_at:1,finished_at:null,status:'completed',reason_code:'',message:'CI visible message 😀',provider:'CI_provider',model:null,reserved_usd:0.26,cost_usd:null,credits:null,result_json};
 const before=JSON.stringify(row),body=JSON.stringify(schedulerAttemptDetails(row));
 if(JSON.stringify(row)!==before)throw new Error('Source unexpectedly mutated selected row');
 process.stdout.write(JSON.stringify({input:before,body})+'\n');count++;
}
process.stderr.write(`Scheduler attempt projection source: ${count} explicit read-set frames; no SQL\n`);
