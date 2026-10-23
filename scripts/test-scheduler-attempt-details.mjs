import assert from 'node:assert/strict';
import {schedulerAttemptDetails as project} from '../server/services/scheduler-attempt-details.js';
const row=(type,result)=>({id:'attempt',candidate_id:123,type,result_json:result});
assert.deepEqual(project(row('classify_title',JSON.stringify({accepted:false,markdown:'PRIVATE'}))).outcome,{accepted:false});
assert.deepEqual(project(row('assign_roles',JSON.stringify({roleIds:[],prompt:'PRIVATE'}))).outcome,{roleIds:[],filtered:true});
assert.deepEqual(project(row('assign_roles',JSON.stringify({roleIds:[2,4],filtered:true}))).outcome,{roleIds:[2,4],filtered:false});
for(const result of ['{','null','[]','{}',JSON.stringify({roleIds:[2,2]}),JSON.stringify({roleIds:['PRIVATE']}),JSON.stringify({roleIds:Array.from({length:13},(_,i)=>i+1)})]){
 const out=project(row('assign_roles',result));assert.equal(out.outcome,undefined);assert.ok(!Object.hasOwn(out,'result_json'));
}
assert.equal(project(row('fetch_full_text',JSON.stringify({markdown:'PRIVATE',accepted:true}))).outcome,undefined);
assert.deepEqual(project(row('import_vacancy',JSON.stringify({superseded:true,nextStage:'assign_roles',markdown:'PRIVATE'}))).outcome,{superseded:true,nextStage:'assign_roles'});
assert.deepEqual(project(row('import_vacancy',JSON.stringify({superseded:true,nextStage:null}))).outcome,{superseded:true,nextStage:null});
assert.equal(project(row('import_vacancy',JSON.stringify({superseded:true,nextStage:'PRIVATE'}))).outcome,undefined);
console.log('Scheduler attempt projection: false/empty decisions, bounded role IDs, malformed/foreign data and body secrecy passed.');
