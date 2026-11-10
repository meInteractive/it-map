import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,render} from './test-support.mjs';

const {default:ParserHHScanSchedule,HHScanScheduleView,isHHScanScheduleReport}=
 await moduleFor('frontend/features/settings/ParserHHScanSchedule.tsx');
const now=Date.parse('2026-11-09T11:00:00Z');
const day=86400000;
const base={
 checkedAt:now,sourceId:'hh',enabled:true,sourceSelected:true,
 favoritesOnly:true,intervalDays:1,vacancyAgeDays:3,
 summary:{configured:8,disabled:1,outsideScope:2,activeScan:1,waiting:3,
  potentiallyDue:1,recalculated:1,unknownLastSuccess:0,nextDueAt:now+day},
 pendingHHScans:1,runningHHScans:0,finalEligibilityVerified:false
};
assert.equal(isHHScanScheduleReport(base),true);
const html=render(HHScanScheduleView,{report:base});
for(const text of ['Срок проверки наступил','раз в 1 дн.','Ожидают срока','МСК',
 'только избранные','уже есть HH-задание','на основе последней успешной проверки']){
 assert.match(html,new RegExp(text,'i'));
}
for(const bad of [
 null,{}, {...base,summary:null}, {...base,summary:{...base.summary,configured:7}},
 {...base,summary:{...base.summary,potentiallyDue:-1}},
 {...base,summary:{...base.summary,nextDueAt:now-1}},
 {...base,finalEligibilityVerified:true}, {...base,intervalDays:5},
 {...base,summary:{...base.summary,nextDueAt:Infinity}},
])assert.equal(isHHScanScheduleReport(bad),false,'invalid schedule report accepted');
const withBreakdown={...base,pendingHHScans:6,pendingBreakdown:{
 pending:6,notDue:1,unverifiedCursor:1,orphanPaid:1,reconcileCandidate:1,
 launchCooldown:1,launchCandidate:1,other:0,oldestPendingAt:now-day
}};
assert.equal(isHHScanScheduleReport(withBreakdown),true);
const breakdownHTML=render(HHScanScheduleView,{report:withBreakdown});
for(const text of ['Предварительная классификация','неподтверждённый курсор',
 'уже оплаченный запуск','ограничение частоты','Самое старое','не проверяет квоты Apify']){
 assert.match(breakdownHTML,new RegExp(text,'i'));
}
for(const bad of [
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,orphanPaid:-1}},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,orphanPaid:2}},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,oldestPendingAt:NaN}},
 {...withBreakdown,pendingHHScans:5},
]){
 assert.equal(isHHScanScheduleReport(bad),false,'invalid HH pending breakdown accepted');
}
const warning={...base,intervalDays:7,vacancyAgeDays:3};
assert.match(render(HHScanScheduleView,{report:warning}),/часть вакансий может быть пропущена/);
assert.doesNotMatch(render(HHScanScheduleView,{report:base}),/часть вакансий может быть пропущена/);
assert.match(render(HHScanScheduleView,{report:{...base,enabled:false}}),/Диспетчер на паузе/);
assert.match(render(HHScanScheduleView,{report:{...base,sourceSelected:false}}),/Источник HH не выбран/);
assert.match(render(HHScanScheduleView,{report:{...base,summary:{configured:0,disabled:0,outsideScope:0,
 activeScan:0,waiting:0,potentiallyDue:0,recalculated:0,unknownLastSuccess:0,nextDueAt:null}}}),
 /не настроено ни одного источника HH/);

const old=Object.fromEntries(['fetch','window','IS_REACT_ACT_ENVIRONMENT'].map(k=>[k,globalThis[k]]));
let root,mode='ok',calls=0;
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
 assert.equal(url,'/api/scheduler/hh-schedule');
 assert.ok(!options.method||options.method==='GET');
 calls++;
 if(mode==='network')throw new Error('Синтетический сетевой сбой');
 if(mode==='broken')return Response.json({...base,summary:null});
 return Response.json(base);
};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const view=()=>JSON.stringify(root.toJSON());
try {
 await act(async()=>{root=create(h(ParserHHScanSchedule));});await flush();await flush();
 assert.equal(calls,1);
 assert.match(view(),/Срок проверки наступил/);
 const refresh=()=>root.root.findAllByType('button').find(b=>b.props.children==='Обновить');
 mode='broken';await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.match(view(),/Некорректный ответ диагностики HH/);
 assert.doesNotMatch(view(),/Потенциально пора проверить/);
 mode='network';await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.match(view(),/Синтетический сетевой сбой/);
 mode='ok';await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.match(view(),/Срок проверки наступил/);
 assert.equal(calls,4);
 console.log('GO-290: read-only HH scheduling report, date gaps, exclusions, malformed JSON, network error and retry passed.');
}finally {
 if(root)await act(async()=>root.unmount());
 Object.assign(globalThis,old);
}
