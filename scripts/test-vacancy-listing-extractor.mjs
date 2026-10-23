import assert from 'node:assert/strict';
import {extractVacancyListing} from '../server/services/vacancy-listing-extractor.js';
const options={pageUrl:'https://career.company.org/jobs',hosts:['career.company.org']};
const r=extractVacancyListing({markdown:'[Главная](/)\n[Архитектор платформы](/jobs/42)\n[Архитектор платформы](/jobs/42?utm_source=test)\n[Подробнее](/jobs/43)\n[Контакты](/contact)\n[Руководитель](/news/manager)\n[Новость](/blog/12)',links:['/jobs/42','/jobs/43','/jobs/44']},options);
assert.equal(r.jobs.length,1);assert.equal(r.jobs[0].title,'Архитектор платформы');assert.equal(r.jobs[0].url,'https://career.company.org/jobs/42');assert.ok(r.diagnostics.unpaired>=2);assert.equal(r.ok,true);
assert.equal(extractVacancyListing({markdown:'# Jobs\nNavigation'},options).ok,false);
assert.equal(extractVacancyListing({markdown:'У компании нет открытых вакансий'},options).emptyConfirmed,true);
const ambiguous=extractVacancyListing({markdown:'[Title A](/jobs/1) [Title B](/jobs/1) [Title A](/jobs/1)'},options);assert.equal(ambiguous.jobs.length,0);assert.equal(ambiguous.diagnostics.ambiguous,1);
const paired=extractVacancyListing({html:'<a href="/jobs/70"><div><h2 class="job-title">Middle &amp; Senior Architect</h2><p>Москва</p></div></a>'},{...options,config:{html:true,titleClass:'job-title'}});assert.equal(paired.jobs[0].title,'Middle & Senior Architect');
const habr=extractVacancyListing({markdown:'[Data Engineer](/vacancies/100000) [Компания](/companies/sample)'},{pageUrl:'https://career.habr.com/companies/sample/vacancies',hosts:['career.habr.com'],config:{adapter:'habr'}});assert.equal(habr.jobs[0].externalJobId,'100000');
const next=extractVacancyListing({markdown:'[Job 1](/jobs/42) [Следующая](/jobs?page=2)'},{...options,config:{pagination:{queryParam:'page'}}});assert.equal(next.nextUrl,'https://career.company.org/jobs?page=2');
const external=extractVacancyListing({markdown:'[Job 1](https://evil.org/jobs/42) [Следующая](https://evil.org/jobs?page=2)'},{...options,config:{pagination:{queryParam:'page'}}});assert.equal(external.jobs.length,0);assert.equal(external.nextUrl,null);
assert.equal(extractVacancyListing({markdown:'[Job 1](/jobs/42) [Следующая](/other?page=2)'},{...options,config:{pagination:{queryParam:'page'}}}).nextUrl,null);
console.log('Listing extractor: coupled title+URL, navigation excluded, no title-based role filter, relative URLs, identity dedupe, conflict exclusion, HTML title class, adapters, true empty/extraction errors and linked bounded pagination passed');

const failedGetmatch=extractVacancyListing({markdown:'###### Не смогли загрузить данные\n##### Вакансий не найдено\n[Старая карточка](/vacancies/old)'},{pageUrl:'https://getmatch.ru/companies/fixture',hosts:['getmatch.ru'],config:{adapter:'getmatch'}});
assert.equal(failedGetmatch.errorCode,'listing_unavailable');assert.equal(failedGetmatch.emptyConfirmed,false);assert.equal(failedGetmatch.ok,false);assert.equal(failedGetmatch.jobs.length,0,'a data-loading failure never imports stale cards or confirms empty');
