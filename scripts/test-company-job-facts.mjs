import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import {moduleFor,render,nav} from './test-support.mjs';
const {CompanyJobFacts}=await moduleFor('frontend/features/vacancies/Vacancies.tsx');
const ui=await moduleFor('scripts/react-test-entry.tsx');
const v={id:81,title:'Facts test',entityType:'agency',companyName:'Agency',realCompanyName:'Employer',role:'Руководитель разработки',level:'Senior',relevance:0,publishedAt:'2026-02-30',loadedAt:'2026-10-04T23:59:00Z',archived:true,applicationId:12,applicationStatus:'Откликнулся',url:'https://example.test/job',telegramSources:[]};
const entities=[{name:'Agency',senior:[300000,400000]}];
const html=render(CompanyJobFacts,{v,entities,type:'agency'});
for(const value of ['~ 300к - 400к','Агентство: Agency','Работодатель: Employer','Релевантность: 0%','Опубликована: ~ 04.10.2026','Загружена: 04.10.2026','Архивная','vacancy-salary-estimate'])assert.ok(html.includes(value),value);
const empty=render(CompanyJobFacts,{v:{...v,entityType:'company',role:'',level:'',relevance:null,loadedAt:null,publishedAt:null,archived:false},entities:[],type:'company'});
assert.match(empty,/Релевантность: —/);assert.match(empty,/Опубликована: —/);assert.doesNotMatch(empty,/Загружена:|Агентство:|Работодатель:|Архивная/);
assert.ok(render(CompanyJobFacts,{v:{...v,salaryFrom:310000,salaryTo:450000,publishedAt:'2026-10-03'},entities,type:'agency'}).includes('310 000 — 450 000 ₽'));
assert.doesNotMatch(render(CompanyJobFacts,{v:{...v,role:'<script>bad</script>'},entities,type:'agency'}),/<script>/);
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
let root;const old=fetch,calls=[];
try {
 globalThis.fetch=async url=>{calls.push(url);return Response.json({vacancies:[v,{...v,id:82,title:'No status',applicationId:null,applicationStatus:'',archived:false,url:'javascript:alert(1)'},{...v,id:83,title:'Hidden',hidden:true}],entities})};
 await act(async()=>{root=create(h(ui.CompanyJobs,{companyName:'Agency',type:'agency',admin:true,nav,onCreate(){},onEdit(){}}))});await act(async()=>{await new Promise(r=>setTimeout(r,0))});
 const rows=root.root.findAll(n=>n.type==='article'&&n.props.className==='job');assert.equal(rows.length,2);
 assert.deepEqual(calls,['/api/vacancies?company=Agency'],'facts require no extra API');
 const source=rows[0].findAllByType('a').find(n=>n.props.href===v.url);assert.ok(source);assert.equal(source.props.rel,'noopener noreferrer');assert.equal(rows[1].findAllByType('a').length,0);
 assert.match(JSON.stringify(rows[0].findByProps({className:'vacancy-status-badge active'}).children),/Откликнулся/);
 assert.deepEqual(rows[1].findByProps({className:'vacancy-status-badge'}).children,['Нет отклика']);
 assert.ok(rows[0].findAllByType('button').some(n=>n.children.includes('Редактировать')));
 console.log('Company job facts: zero/missing relevance, date fallback, agency/employer, salaries, archive/status, safe source links and no extra API passed.');
} finally {if(root)await act(async()=>root.unmount());ui.clearCache();globalThis.fetch=old;}
