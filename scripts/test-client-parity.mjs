import assert from 'node:assert/strict';
import {moduleFor,render,nav} from './test-support.mjs';
const {comparePublication,compareVacancies,vacancySalary,publicationLabel}=await moduleFor('frontend/features/vacancies/model.ts');
const {VacancyTable}=await moduleFor('frontend/features/vacancies/Vacancies.tsx');
const {revenue,companySalary,SalaryLines}=await moduleFor('frontend/features/companies/CompanyList.tsx');
const {Offices}=await moduleFor('frontend/features/companies/Offices.tsx');
const {ContactLinks}=await moduleFor('frontend/features/companies/Contacts.tsx');
const base={companyName:'Employer',entityType:'company',title:'Engineer',role:'',level:'Middle',telegramSources:[],salaryFrom:null,salaryTo:null};
assert.equal(vacancySalary({...base,salaryFrom:200000,salaryTo:300000},[]).value,'200 000 — 300 000 ₽');
const entities=[{name:'Employer',middle:[200000,300000]},{name:'Agency',middle:[220000,330000]}];
assert.equal(vacancySalary(base,entities).value,'~ 200к - 300к');
const agency={...base,entityType:'agency',companyName:'Agency',realCompanyName:'Employer'};
assert.equal(vacancySalary(agency,entities,'agency').value,'~ 220к - 330к');
assert.equal(vacancySalary(agency,entities,'company').value,'~ 200к - 300к');
assert.equal(publicationLabel({...base,loadedAt:'2026-10-02'}),'~ 02.10.2026');
assert.equal(publicationLabel({...base,publishedAt:'2026-02-30',loadedAt:'2026-10-02'}),'~ 02.10.2026');
assert.ok(comparePublication({id:1,publishedAt:'2026-10-02',createdAt:'2026-10-03'},{id:2,publishedAt:'2026-10-02',createdAt:'2026-10-02'})<0,'Tie by createdAt before id');
const vacancies=[{id:10,companyName:'No response'},{id:9,companyName:'Other vacancy only'},{id:4,companyName:'Ozon'},{id:3,companyName:'Ozon'},{id:2,companyName:'Agency'},{id:1,companyName:'Direct',applicationId:123}];
const ids=(general,sort='application')=>[...vacancies].sort((a,b)=>compareVacancies(a,b,sort,'company',new Set(general))).map(v=>v.id);
assert.deepEqual(ids(['Ozon','Agency']),[4,3,2,1,10,9]);assert.deepEqual(ids(['Agency']),[2,1,10,9,4,3]);assert.deepEqual(ids([]),[1,10,9,4,3,2]);assert.deepEqual(ids([],'date'),[10,9,4,3,2,1]);assert.ok(!vacancies.find(v=>v.companyName==='Ozon').applicationId);
const html=render(VacancyTable,{records:[{...base,id:1,loadedAt:'2026-10-02',archived:true}],entities,type:'company',admin:false,nav});
for(const label of ['Название','Роль','Компания','Вилка зарплат','Уровень','Релевантность','Статус отклика','Дата публикации','Действия'])assert.ok(html.includes('<th>'+label+'</th>'));
assert.match(html,/vacancy-col-title/);assert.match(html,/vacancy-archive-badge/);assert.match(html,/Архивная/);assert.ok(!html.includes('job-delete'));assert.match(html,/~ 02.10.2026/);
assert.equal(revenue(null),'нет данных в ₽');assert.equal(revenue(0),'0 ₽');assert.equal(revenue(1.25e12),'1,25 трлн ₽');
assert.equal(companySalary([200000,300000]),'200к - 300к');
assert.match(render(SalaryLines,{entity:{entityType:'agency',middle:[200000,300000],salaryRangeSource:{middle:'vacancies'}}}),/~ 200к - 300к/);
const offices=render(Offices,{entity:{offices:[{address:'Test',metro:'Белорусская; Савёловская'}]}});assert.match(offices,/office-metro-chip/);assert.match(offices,/office-metro-chip/);assert.match(offices,/--station-fill/);
// Empty contact fields must not create clickable no-op mailto:/tel: links.
const emptyContact={position:1,fullName:'Recruiter',linkedinUrl:'',facebookUrl:'',setkaUrl:'',telegram:'',phone:'',email:'',specialization:''};
const contactHtml=fields=>render(ContactLinks,{contact:{...emptyContact,...fields}});
const emptyLinks=contactHtml({});assert.equal((emptyLinks.match(/<a\b/g)||[]).length,0);assert.match(emptyLinks,/Способы связи пока не указаны/);
const phoneLinks=contactHtml({phone:' +7 (999) 123-45-67 '});assert.equal((phoneLinks.match(/<a\b/g)||[]).length,1);assert.ok(phoneLinks.includes('href="tel:+79991234567"'));assert.ok(!phoneLinks.includes('mailto:'));assert.ok(!phoneLinks.includes('target="_blank"'));
const emailLinks=contactHtml({email:' recruiter@example.com '});assert.equal((emailLinks.match(/<a\b/g)||[]).length,1);assert.ok(emailLinks.includes('href="mailto:recruiter@example.com"'));assert.ok(!emailLinks.includes('tel:'));
const allLinks=contactHtml({linkedinUrl:'https://linkedin.com/in/demo',facebookUrl:'https://facebook.com/demo',setkaUrl:'https://setka.ru/demo',telegram:' @demo_user ',phone:'+7 999 1234567',email:'recruiter@example.com'});
assert.equal((allLinks.match(/<a\b/g)||[]).length,6);assert.equal((allLinks.match(/target="_blank"/g)||[]).length,4);assert.ok(allLinks.includes('href="https://t.me/demo_user"'));assert.match(allLinks,/Telegram: @demo_user/);
const invalidLinks=contactHtml({linkedinUrl:'javascript:alert(1)',facebookUrl:'data:text/html,unsafe',setkaUrl:'mailto:wrong@example.com',telegram:'bad user',phone:'call me',email:'  '});assert.equal((invalidLinks.match(/<a\b/g)||[]).length,0);assert.match(invalidLinks,/Способы связи пока не указаны/);
console.log('Historical UI contracts: table headers, dates/ties, salaries, agency estimates, application sorting, archive badge, metro chips, revenue and non-empty safe contact links.');

// Channel details use the existing API check timestamp; compact cards preserve publication fallback and escaping.
const {ChannelDetails,ChannelVacancies}=await moduleFor('frontend/features/channels/Channels.tsx');
const channel={id:1,name:'Test',url:'https://t.me/test',username:'@test',lastSuccessfulCheckAt:'2026-10-06T09:00:00Z',comment:'',vacancyCount:1};
const details=render(ChannelDetails,{channel});assert.match(details,/@test/);assert.match(details,/06\.10\.2026, 12:00/);
assert.ok(!render(ChannelDetails,{channel:{...channel,lastSuccessfulCheckAt:null}}).includes('Invalid Date'));
const channelCards=render(ChannelVacancies,{records:[{...base,id:8,title:'<Engineer>',loadedAt:'2026-10-02'}],nav});
assert.match(channelCards,/&lt;Engineer&gt;/);assert.match(channelCards,/~ 02\.10\.2026 · Employer/);
assert.match(render(ChannelVacancies,{records:[],nav}),/Связанных вакансий пока нет/);

// Info uses the detail already loaded, including agency salary context and publication fallback.
const {VacancyInfo}=await moduleFor('frontend/features/vacancies/VacancyDrawer.tsx');
const info=render(VacancyInfo,{v:{...agency,id:11,role:'Менеджер проектов',relevance:0,archived:true,createdAt:'2026-10-06T09:00:00Z',loadedAt:'2026-10-02',applicationId:3,applicationStatus:'Отказ',telegramSources:[{channelId:7,channelName:'Test <channel>',telegramContact:'@recruiter'}]},entities,view:'agency',nav});
for(const value of ['~ 220к - 330к','Агентство: Agency','Работодатель: Employer','Релевантность: 0%','Опубликована: ~ 02.10.2026','Загружена: 02.10.2026','@recruiter','Отказ','Архивная','Test &lt;channel&gt;'])assert.ok(info.includes(value),value);
const unknownInfo=render(VacancyInfo,{v:{...base,telegramSources:[]},entities:[],view:'company',nav});
assert.ok(unknownInfo.includes('Релевантность: —'));assert.ok(unknownInfo.includes('Нет отклика'));assert.ok(!unknownInfo.includes('Invalid Date'));

// Review sources remain external web links, with useful empty state and React escaping.
const {ReviewSources}=await moduleFor('frontend/features/companies/CompanyDrawer.tsx');
const reviewLinks=render(ReviewSources,{value:{dreamJob:' https://example.test/reviews ',unsafe:'javascript:alert(1)',email:'mailto:bad@example.test','<source>':'https://example.test/other'}});
assert.equal((reviewLinks.match(/<a\b/g)||[]).length,2);assert.match(reviewLinks,/DreamJob/);assert.match(reviewLinks,/href="https:\/\/example.test\/reviews"/);assert.match(reviewLinks,/&lt;source&gt;/);
assert.ok(!reviewLinks.includes('javascript:'));assert.ok(!reviewLinks.includes('mailto:'));assert.match(render(ReviewSources,{value:{}}),/Источники отзывов пока не добавлены/);

// Map suggestions and GeoJSON labels share the historical rank format.
const {mapCompanyLabel}=await moduleFor('frontend/features/map/labels.ts');
assert.equal(mapCompanyLabel({name:'Synthetic',rank:12}),'№ 12 Synthetic');
assert.equal(mapCompanyLabel({name:'Synthetic',rank:'12'}),'№ 12 Synthetic');
for(const rank of [null,undefined,0,-1,1.5,Infinity,NaN])assert.equal(mapCompanyLabel({name:'Synthetic',rank}),'Synthetic');
assert.equal(mapCompanyLabel({name:'Name <with> Unicode: Ё',rank:301}),'№ 301 Name <with> Unicode: Ё');

// Legend colors follow the route source data in the historical label order.
const {MapLegend}=await moduleFor('frontend/features/map/Map.tsx');
const legend=render(MapLegend);
const routeFiles=['zamoskvoretskaya','mcd3','lyublinsko-dmitrovskaya','arbatsko-pokrovskaya'];
const {readFile}=await import('node:fs/promises');
const routes=await Promise.all(routeFiles.map(async file=>JSON.parse(await readFile('src/'+file+'.json','utf8'))));
assert.deepEqual(routes.map(r=>r.code),['2','D3','10','3']);
assert.deepEqual([...legend.matchAll(/<i style="background:([^"]+)"/g)].map(m=>m[1]),routes.map(r=>r.color));
for(const value of ['офисы','метро и МЦК','линии 2, D3, 10 и 3','https://openfreemap.org/','https://www.openstreetmap.org/copyright'])assert.ok(legend.includes(value),value);

// Company surfaces use a prefix only for valid positions; Common.rank is shared
// with applications that add their own prefix and must remain unchanged.
const {companyRankLabel}=await moduleFor('frontend/features/companies/labels.ts');
const {rank:bareRank}=await moduleFor('frontend/components/Common.tsx');
assert.equal(companyRankLabel(12),'№ 12');assert.equal(companyRankLabel('12'),'№ 12');
for(const value of [undefined,null,'',0,-1,1.5,Infinity,NaN,'invalid'])assert.equal(companyRankLabel(value),'');
assert.equal(bareRank(12),'12');

// D1/SQLite timestamps are UTC even without an offset; display must not depend
// on the browser/host timezone. Explicit ISO offsets remain meaningful.
const {dateLabel}=await moduleFor('frontend/components/Common.tsx');
const {History}=await moduleFor('frontend/features/applications/Applications.tsx');
const previousTimezone=process.env.TZ;
try {
 for(const timezone of ['Europe/Moscow','UTC','America/Los_Angeles']){
  process.env.TZ=timezone;
  const expected='06.10.2026, 13:00';
  for(const value of ['2026-10-06 10:00:00','2026-10-06 10:00:00.123','2026-10-06T10:00:00Z','2026-10-06T13:00:00+03:00','2026-10-06T06:00:00-04:00'])assert.equal(dateLabel(value),expected,timezone+': '+value);
  assert.equal(dateLabel('2026-10-06 21:30:00'),'07.10.2026, 00:30',timezone+' midnight');
  for(const value of [null,undefined,'','invalid'])assert.equal(dateLabel(value),'—');
  const history=render(History,{record:{history:[{fromStatus:'',toStatus:'Офер',changedAt:'2026-10-06 10:00:00'}]}});
  assert.ok(history.includes(expected),timezone+' actual history');
 }
}finally{if(previousTimezone===undefined)delete process.env.TZ;else process.env.TZ=previousTimezone;}

// History conveys the actual transition, keeps API order, and safely renders
// statuses even when a restored/imported record contains markup-like text.
const historyHtml=render(History,{record:{history:[
 {fromStatus:'Собеседование',toStatus:'Офер',changedAt:'2026-10-06 10:00:00'},
 {fromStatus:'',toStatus:'Откликнулся',changedAt:'2026-10-05 10:00:00'},
 {fromStatus:'Откликнулся',toStatus:'',changedAt:null},
 {fromStatus:'<previous>',toStatus:'<script>unsafe</script>',changedAt:'invalid'},
]}});
assert.ok(historyHtml.includes('06.10.2026, 13:00 · Собеседование → Офер'));
assert.ok(historyHtml.includes('05.10.2026, 13:00 · Нет отклика → Откликнулся'));
assert.ok(historyHtml.includes('— · Откликнулся → Нет отклика'));
assert.ok(historyHtml.indexOf('Собеседование → Офер')<historyHtml.indexOf('Нет отклика → Откликнулся'));
assert.ok(historyHtml.includes('&lt;previous&gt; → &lt;script&gt;unsafe&lt;/script&gt;'));
assert.ok(!historyHtml.includes('<script>'));
assert.match(render(History,{record:{history:[]}}),/История пока пустая/);
