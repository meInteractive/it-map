// Real React + protected production HTTP/native D1 lost-response regression.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {fileURLToPath} from 'node:url';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {prepareBackend,moduleFor} from './test-support.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
await prepareBackend();const ui=await moduleFor('scripts/react-test-entry.tsx');
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const options={cf:false,modules:true,scriptPath:fileURLToPath(new URL('../.tests/worker.mjs',import.meta.url)),compatibilityDate:'2026-05-15',d1Databases:{DB:'lost-application-response'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'synthetic-secret'},outboundService:()=>new Response('Disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
const oldFetch=fetch;let root,dropAck=true,saved=0;const sent=[],statuses=[];let contextGets=0;
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const seed=JSON.parse(fs.readFileSync('src/data.json','utf8'));await db.prepare('INSERT INTO app_catalog(id,data_json) VALUES(1,?)').bind(JSON.stringify({...seed,companies:[{...seed.companies[0],name:'Synthetic',jobs:[]}],agencies:[]})).run();await db.prepare("INSERT INTO company_crm(company_name) VALUES('Synthetic')").run();
 const auth=await mf.dispatchFetch('https://lost.test/auth',{method:'POST',body:new URLSearchParams({login:'Nastya',password:'synthetic'}),redirect:'manual'});assert.equal(auth.status,303);const cookie=auth.headers.get('set-cookie').split(';')[0];
 globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications/create-context'){
   contextGets++;return mf.dispatchFetch('https://lost.test'+url,{headers:{cookie}});
  }
  assert.equal(url,'/api/applications/create');assert.equal(options.method,'POST');sent.push(JSON.parse(options.body));
  const response=await mf.dispatchFetch('https://lost.test'+url,{method:options.method,body:options.body,headers:{cookie,origin:'https://lost.test','content-type':'application/json'}});statuses.push(response.status);
  if(dropAck&&response.ok){dropAck=false;throw new TypeError('Synthetic lost response after commit');}
  return response;
 };
 const submit=()=>act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
 const waitReady=async()=>{for(let i=0;i<500&&root.root.findByProps({className:'crm-save'}).props.disabled;i++)await act(async()=>{await new Promise(resolve=>setTimeout(resolve,2));});assert.equal(root.root.findByProps({className:'crm-save'}).props.disabled,false);};
 const mount=async comment=>{await act(async()=>{root=create(h(ui.ApplicationForm,{companyName:'Synthetic',onSaved(){saved++;}}));});await waitReady();await act(async()=>root.root.findAllByType('input').find(input=>input.props.type==='checkbox').props.onChange({target:{checked:true}}));await act(async()=>root.root.findByType('textarea').props.onChange({target:{value:comment}}));};
 const rows=async()=>({applications:(await db.prepare("SELECT * FROM company_applications WHERE user_name='Nastya'").all()).results,history:(await db.prepare('SELECT * FROM company_application_status_history').all()).results});
 await mount('Synthetic retained draft');await submit();let state=await rows();assert.equal(state.applications.length,1,JSON.stringify({statuses,sent,ui:root.toJSON()}));assert.equal(state.history.length,1);assert.equal(saved,0);assert.equal(root.root.findByType('textarea').props.value,'Synthetic retained draft');assert.match(JSON.stringify(root.toJSON()),/lost response after commit/);
 await submit();state=await rows();assert.equal(state.applications.length,1);assert.equal(state.history.length,1);assert.equal(saved,1);assert.deepEqual(sent[0],sent[1]);assert.deepEqual(statuses,[201,200]);assert.equal(contextGets,1);await act(async()=>root.unmount());root=null;ui.clearCache();
 console.log(JSON.stringify({id:'S03c',actualReactAndWorkerdD1:true,firstSuccessLost:true,retainedDraft:true,identicalRetryCreatedDuplicate:false,applications:1,historyRows:1}));
 // Lost success for the tenth record also replays before the max10 check.
 await db.batch(Array.from({length:8},()=>db.prepare("INSERT INTO company_applications(user_name,company_name,status,comment_md) VALUES('Nastya','Synthetic','','Synthetic initial row')")));
 dropAck=true;saved=0;await mount('Synthetic tenth draft');await submit();assert.equal((await rows()).applications.length,10);assert.equal(saved,0);await submit();assert.deepEqual(statuses.slice(-2),[201,200]);assert.equal((await rows()).applications.length,10);assert.equal(saved,1);assert.equal(contextGets,2);assert.notEqual(sent[0].idempotencyKey,sent[2].idempotencyKey);
 console.log(JSON.stringify({id:'S03c',tenthRecordActuallySaved:true,retryRejectedByLimit:false,userGetsSuccess:true,receiptReplayBeforeCountCheck:true}));
 const adminAuth=await mf.dispatchFetch('https://lost.test/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});assert.equal(adminAuth.status,303);
 const backupResponse=await mf.dispatchFetch('https://lost.test/api/backup',{headers:{cookie:adminAuth.headers.get('set-cookie').split(';')[0]}});assert.equal(backupResponse.status,200);const backup=await backupResponse.json();
 await act(async()=>root.unmount());root=null;dropAck=false;saved=0;await mount('Synthetic stale draft');
 const api=await import('../.tests/backend.mjs');await api.restoreBackup(db,backup);const beforeRestoreSubmit=await rows();await submit();assert.equal(statuses.at(-1),409);assert.deepEqual(await rows(),beforeRestoreSubmit);assert.equal(saved,0);assert.equal(contextGets,3);assert.equal(root.root.findByType('textarea').props.value,'Synthetic stale draft');
 console.log('Real React/HTTP/D1: lost ACK retries exactly one application/history, tenth-slot replay, fresh intent for new draft and frozen stale context after identical restore passed.');
}finally{if(root)await act(async()=>root.unmount());ui.clearCache();globalThis.fetch=oldFetch;await mf.dispose();}
