// S05: real detail subscription + mutation invalidation + cover-letter component.
import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
const old=fetch,oldWindow=globalThis.window;let root,postCount=0,getCount=0,held,regenCalls=0,regenFail=false;
let detail={id:811,coverLetterSubject:'',coverLetterMd:'',coverLetterScope:'personal'};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const done=letter=>new Response(JSON.stringify({type:'progress',elapsedSeconds:180,model:'Synthetic'})+'\n'+JSON.stringify({type:'done',letter})+'\n',{headers:{'content-type':'application/x-ndjson'}});
function Panel(){const q=ui.useApi('/api/vacancies/811');return q.data?h(ui.CoverLetter,{v:q.data.vacancy,ready:!q.loading&&!q.error}):null;}
const mount=async()=>{await act(async()=>{root=create(h(Panel))});};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=undefined;};
try{
 globalThis.fetch=async(url,o={})=>{
  if(o.method==='POST'&&url.endsWith('/cover-letter/regenerate')){regenCalls++;if(held)return held.promise;if(regenFail)return new Response(JSON.stringify({type:'error',error:'Synthetic regeneration failure'})+'\n');detail={...detail,coverLetterSubject:'Regenerated subject',coverLetterMd:'REGENERATED PERSONAL'};return done(detail);}
  if(o.method==='POST'&&url.endsWith('/cover-letter')){postCount++;if(held)return held.promise;detail={...detail,coverLetterSubject:'Generated subject',coverLetterMd:'GENERATED PERSONAL'};return done(detail);}
  if(o.method==='PUT'){const {coverLetterSubject,coverLetterMd,coverLetterScope}=JSON.parse(o.body);detail={...detail,coverLetterSubject,coverLetterMd,coverLetterScope};return Response.json({ok:true});}
  if(o.method==='POST')return Response.json({ok:true});
  getCount++;return Response.json({vacancy:detail});
 };
 await mount();assert.match(label(root.root),/GENERATED PERSONAL/);assert.equal(postCount,1);assert.equal(getCount,2);
 await act(async()=>ui.mutate('/api/vacancies/811','PUT',{coverLetterScope:'personal',coverLetterSubject:'Edited subject',coverLetterMd:'EDITED PERSONAL'}));
 assert.match(label(root.root),/Edited subject/);assert.match(label(root.root),/EDITED PERSONAL/);assert.doesNotMatch(label(root.root),/GENERATED PERSONAL/);assert.equal(postCount,1);
 await stop();await mount();assert.match(label(root.root),/EDITED PERSONAL/);assert.equal(postCount,1);assert.equal(getCount,3);
 await act(async()=>ui.mutate('/api/vacancies/811','PUT',{coverLetterScope:'shared',coverLetterSubject:'Shared subject',coverLetterMd:'SHARED LETTER'}));
 assert.match(label(root.root),/SHARED LETTER/);assert.doesNotMatch(label(root.root),/EDITED PERSONAL/);assert.equal(postCount,1);
 // Existing letters regenerate only on explicit click; old text remains on failure/in flight.
 const createButton=()=>root.root.findByProps({'aria-label':'Создать сопроводительное письмо заново'});
 assert.equal(createButton().children.join(''),'Создать');assert.equal(regenCalls,0);
 regenFail=true;await act(async()=>createButton().props.onClick());assert.equal(regenCalls,1);assert.match(label(root.root),/Synthetic regeneration failure/);assert.match(label(root.root),/SHARED LETTER/);
 regenFail=false;let releaseRegen;held={promise:new Promise(resolve=>releaseRegen=resolve)};
 await act(async()=>createButton().props.onClick());assert.equal(createButton().props.disabled,true);assert.match(label(root.root),/SHARED LETTER/);assert.equal(regenCalls,2);
 let duplicated;await act(async()=>{duplicated=ui.generateLetter(811,{regenerate:true});});assert.equal(regenCalls,2,'Double click shares in-flight request');
 detail={...detail,coverLetterSubject:'Regenerated subject',coverLetterMd:'REGENERATED PERSONAL'};
 await act(async()=>{releaseRegen(done(detail));await duplicated;});held=undefined;
 assert.match(label(root.root),/Regenerated subject/);assert.match(label(root.root),/REGENERATED PERSONAL/);assert.doesNotMatch(label(root.root),/SHARED LETTER/);assert.equal(createButton().props.disabled,false);
 await stop();await mount();assert.equal(regenCalls,2,'Opening a saved letter does not regenerate');
 // Clearing the saved letter really removes the old text and permits one new generation.
 await act(async()=>ui.mutate('/api/vacancies/811','PUT',{coverLetterScope:'personal',coverLetterSubject:'',coverLetterMd:''}));
 assert.match(label(root.root),/GENERATED PERSONAL/);assert.doesNotMatch(label(root.root),/SHARED LETTER/);assert.equal(postCount,2);
 detail={...detail,coverLetterSubject:'Restored subject',coverLetterMd:'RESTORED LETTER'};
 await act(async()=>ui.mutate('/api/backup','POST',{}));
 assert.match(label(root.root),/RESTORED LETTER/);assert.doesNotMatch(label(root.root),/GENERATED PERSONAL/);assert.equal(postCount,2);
 await stop();
 // No letter text/status crosses session reset. Late done from an old stream is ignored.
 let release;held={promise:new Promise(r=>release=r)};
 let pending;await act(async()=>{pending=ui.generateLetter(812)});
 await act(async()=>ui.clearCache());
 await act(async()=>{release(done({coverLetterSubject:'OLD SESSION',coverLetterMd:'PRIVATE OLD SESSION'}));await pending});held=undefined;
 await act(async()=>{root=create(h(ui.CoverLetter,{v:{id:812,coverLetterSubject:'Current subject',coverLetterMd:'CURRENT SAVED'},ready:false}))});
 assert.doesNotMatch(label(root.root),/OLD SESSION|Письмо сохранено/);await stop();
 detail={...detail,coverLetterSubject:'Other candidate',coverLetterMd:'OTHER CANDIDATE'};
 await mount();assert.match(label(root.root),/OTHER CANDIDATE/);assert.doesNotMatch(label(root.root),/GENERATED PERSONAL|PRIVATE OLD SESSION|Письмо сохранено/);
 // A fresh detail (including an empty result) is always rendered, even without mutation events.
 await act(async()=>root.update(h(ui.CoverLetter,{v:{...detail,coverLetterSubject:'',coverLetterMd:''},ready:false})));
 assert.equal(root.root.findAllByProps({'aria-label':'Создать сопроводительное письмо заново'}).length,0,'Empty letters have no regeneration button');
 assert.doesNotMatch(label(root.root),/OTHER CANDIDATE|GENERATED PERSONAL/);assert.match(label(root.root),/Письмо пока не создано/);
 await stop();
 // A stale 401 must not expire a new session; an active 401 must still expire it.
 const sessionEvents=[];globalThis.window={dispatchEvent(event){sessionEvents.push(event.type);return true;}};
 let release401;globalThis.fetch=async()=>{await new Promise(r=>release401=r);return Response.json({error:'OLD UNAUTHORIZED'},{status:401});};
 let oldSessionRequest;await act(async()=>{oldSessionRequest=ui.generateLetter(813)});
 await act(async()=>ui.clearCache());await act(async()=>{release401();await oldSessionRequest});
 assert.deepEqual(sessionEvents,[],'Late 401 belongs to the invalidated session');
 await act(async()=>{root=create(h(ui.CoverLetter,{v:{id:813,coverLetterSubject:'New session',coverLetterMd:'NEW SESSION LETTER'},ready:false}))});
 assert.match(label(root.root),/NEW SESSION LETTER/);assert.doesNotMatch(label(root.root),/OLD UNAUTHORIZED/);await stop();
 let releaseErrorBody;globalThis.fetch=async()=>{const response=Response.json({error:'OLD BODY'},{status:401});response.json=()=>new Promise(r=>releaseErrorBody=r);return response;};
 let oldBodyRequest;await act(async()=>{oldBodyRequest=ui.generateLetter(815)});assert.equal(typeof releaseErrorBody,'function');
 await act(async()=>ui.clearCache());await act(async()=>{releaseErrorBody({error:'OLD BODY'});await oldBodyRequest});assert.deepEqual(sessionEvents,[],'Invalidated while reading error body');
 globalThis.fetch=async()=>Response.json({error:'CURRENT UNAUTHORIZED'},{status:401});
 await act(async()=>ui.generateLetter(814));assert.deepEqual(sessionEvents,['session-expired']);
 console.log('PASS S05: generated → edited → reopen; shared/personal/clear; session reset and late stream; one saved-text cache');
}finally{await stop();ui.clearCache();globalThis.fetch=old;if(oldWindow===undefined)delete globalThis.window;else globalThis.window=oldWindow;}
