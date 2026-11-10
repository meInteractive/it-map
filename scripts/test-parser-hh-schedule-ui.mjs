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
 pending:6,notDue:1,unverifiedCursor:1,unverifiedLineage:1,missingOperation:0,
 orphanPaid:1,reconcileCandidate:1,
 launchCooldown:1,launchCandidate:1,other:0,oldestPendingAt:now-day,
 oldestUnverifiedAt:now-day,oldestReconcileAt:now-day,
 oldestCooldownAt:now-day,
}};
assert.equal(isHHScanScheduleReport(withBreakdown),true);
const breakdownHTML=render(HHScanScheduleView,{report:withBreakdown});
for(const text of ['Предварительная классификация','неподтверждённый курсор',
 'существующий запуск','ограничение частоты','Самое старое','не проверяет квоты Apify',
 'происхождение или версия источника','Старейшее дочитывание Actor']){
 assert.match(breakdownHTML,new RegExp(text,'i'));
}
for(const bad of [
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,orphanPaid:-1}},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,orphanPaid:2}},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,oldestPendingAt:NaN}},
 {...withBreakdown,pendingHHScans:5},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,missingOperation:1}},
 {...withBreakdown,pendingBreakdown:{...withBreakdown.pendingBreakdown,oldestUnverifiedAt:-1}},
]){
 assert.equal(isHHScanScheduleReport(bad),false,'invalid HH pending breakdown accepted');
}
const withCoverage={...base,summary:{...base.summary,
 successfulLast24h:2,successfulLast48h:4,successDateUnknown:1,
 noSuccess48hWaiting:1,noSuccess48hActive:0,noSuccess48hDue:0,
 noSuccess48hWaitPartial:1}};
assert.equal(isHHScanScheduleReport(withCoverage),true);
const coverageHTML=render(HHScanScheduleView,{report:withCoverage});
assert.match(coverageHTML,/Успешно проверено HH-источников за последние 24 часа/);
assert.match(coverageHTML,/За 48 часов/);
assert.match(coverageHTML,/полностью завершённый/);
assert.match(coverageHTML,/Без подтверждённого полного обхода за 48 часов/);
assert.match(coverageHTML,/после частичного результата/);
for(const bad of [
 {...withCoverage,summary:{...withCoverage.summary,successfulLast24h:5}},
 {...withCoverage,summary:{...withCoverage.summary,successDateUnknown:4}},
 {...withCoverage,summary:{...withCoverage.summary,successfulLast24h:-1}},
 {...withCoverage,summary:{...withCoverage.summary,successfulLast48h:undefined}},
 {...withCoverage,summary:{...withCoverage.summary,successDateUnknown:NaN}},
 {...withCoverage,summary:{...withCoverage.summary,noSuccess48hWaiting:0}},
 {...withCoverage,summary:{...withCoverage.summary,noSuccess48hWaitPartial:2}},
 {...withCoverage,summary:{...withCoverage.summary,noSuccess48hActive:1}},
 {...withCoverage,summary:{...withCoverage.summary,noSuccess48hWaiting:undefined}},
]){
 assert.equal(isHHScanScheduleReport(bad),false,'invalid HH confirmed-coverage report accepted');
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
const oldNavigator=Object.getOwnPropertyDescriptor(globalThis,'navigator');
const writes=[];
let authorized=true;
Object.defineProperty(globalThis,'navigator',{configurable:true,value:{
 clipboard:{async writeText(text){writes.push(JSON.parse(text));}},
}});
let root,mode='ok',calls=0;
globalThis.window={addEventListener(){},removeEventListener(){}};
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.fetch=async(url,options={})=>{
 assert.equal(url,'/api/scheduler/hh-schedule');
 assert.ok(!options.method||options.method==='GET');
 calls++;
 if(mode==='network')throw new Error('Синтетический сетевой сбой');
 if(mode==='broken')return Response.json({...base,summary:null});
 if(mode==='full')return Response.json({...withBreakdown,summary:{...withBreakdown.summary,
  successfulLast24h:2,successfulLast48h:4,successDateUnknown:1},
  companyName:'CI_PRIVATE_COMPANY',accessToken:'CI_PRIVATE_TOKEN'});
 return Response.json(base);
};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const view=()=>JSON.stringify(root.toJSON());
try {
 await act(async()=>{root=create(h(ParserHHScanSchedule,{isAuthorized:()=>authorized}));});await flush();await flush();
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
 // Кнопка копирует кэшированный read-only снимок; новые запросы не выполняет.
 mode='full';await act(async()=>refresh().props.onClick());await flush();await flush();
 const copy=()=>root.root.findByProps({'aria-label':'Копировать JSON'});
 assert.equal(copy().props.disabled,false);
 const beforeCopy=calls;
 await act(async()=>copy().props.onClick());
 assert.equal(calls,beforeCopy,'копирование не запрашивает API и не запускает Actor');
 assert.equal(writes.length,1);
 assert.equal(writes[0].section,'parser_hh_schedule');
 assert.equal(writes[0].summary.successfulLast24h,2);
 assert.equal(writes[0].summary.successfulLast48h,4);
 assert.equal(writes[0].summary.pendingBreakdown.pending,6);
 assert.equal(writes[0].summary.pendingBreakdown.unverifiedCursor,1);
 assert.equal(writes[0].summary.pendingBreakdown.reconcileCandidate,1);
 assert.equal(writes[0].summary.pendingBreakdown.oldestPendingAt,new Date(now-day).toISOString());
 assert.equal(writes[0].dataAsOf,new Date(now).toISOString());
 assert.equal(writes[0].filters.intervalDays,1);
 assert.equal(writes[0].summary.finalEligibilityVerified,false);
 assert.ok(!JSON.stringify(writes[0]).includes('CI_PRIVATE'));
 authorized=false;
 await act(async()=>copy().props.onClick());
 assert.equal(writes.length,1,'права проверяются в момент копирования');
 authorized=true;
 mode='broken';await act(async()=>refresh().props.onClick());await flush();await flush();
 assert.equal(copy().props.disabled,true,'некорректный ответ нельзя скопировать как достоверный');
 console.log('GO-439: HH JSON clipboard from cached GET, coverage, pending causes, no private fields, admin and error guards.');
 console.log('GO-290: read-only HH scheduling report, date gaps, exclusions, malformed JSON, network error and retry passed.');
}finally {
 if(root)await act(async()=>root.unmount());
 Object.assign(globalThis,old);
 if(oldNavigator)Object.defineProperty(globalThis,'navigator',oldNavigator);
 else delete globalThis.navigator;
}
