import assert from 'node:assert/strict';
import fs from 'node:fs';
import { runInNewContext } from 'node:vm';

import {prepareBackend} from './test-support.mjs';
await prepareBackend();
const context=await import('../.tests/backend.mjs');
const headers=context.COMPANY_IMPORT_EXPORT_HEADERS;
const baseline = {
  ratingFormulaVersion:4, wlbScore:100, rating:5,
  ratingInputs:{roleFit:20,deferment:10,transparency:5,office:5},
  middle:[260000,300000],senior:[310000,400000],revenue:{amount:1e12},
  confidence:'Высокая',offices:[{status:'Подтверждено'}]
};
const rate = company => context.calculateCompanyRating(company);
assert.equal(rate(baseline).score,90);
assert.equal(rate({...baseline,aiScore:5,aiEvidence:'Профильные вакансии; источник'}).score,95);
assert.equal(rate({...baseline,aiScore:10,aiEvidence:'Действующее ИИ-направление; источник'}).score,100);
assert.equal(rate({...baseline,aiScore:10}).score,90);
assert.equal(rate({...baseline,aiScore:7,aiEvidence:'Источник'}).score,90);
assert.equal(rate({...baseline,offices:[],confidence:'Низкая',ratingInputs:{roleFit:20,deferment:10,office:0,transparency:0}}).score,90);
assert.equal('office' in rate(baseline).ratingBreakdown,false);
assert.equal('transparency' in rate(baseline).ratingBreakdown,false);
// A formula upgrade must not reinterpret the existing 0–100 WLB scale as 0–20.
for (const value of [0,20,37,80,100]) {
  const upgraded = rate({...baseline,wlbScore:value});
  assert.equal(upgraded.wlbScore,value);
  assert.equal(upgraded.ratingBreakdown.wlb,Math.round(value/5));
  assert.equal(rate({...baseline,...upgraded,ratingFormulaVersion:5}).score,upgraded.score);
}
const update = context.companyAiImportUpdate;
assert.equal(update({}, {aiScore:10,aiEvidence:'Source'}).aiScore,10);
assert.equal(update({aiScore:0},{aiScore:10,aiEvidence:'Source'}).aiScore,0);
assert.equal(update({aiScore:'5',aiEvidence:'Vacancy source'}).aiScore,5);
assert.throws(()=>update({aiScore:7,aiEvidence:'Source'}));
assert.throws(()=>update({aiScore:10}));
const values = context.companyImportExportValues({
  ...baseline,
  aiScore:5,
  aiEvidence:'Source',
  vacancySources:{dreamJob:'https://dreamjob.ru/employers/example/vakansii',hCareers:'https://h.careers/companies/example'}
},1);
assert.equal(values.length,headers.length);
assert.equal(values[headers.indexOf('Балл ИИ')],5);
assert.equal(values[headers.indexOf('ИИ — обоснование и источники')],'Source');
assert.equal(values[headers.indexOf('DreamJob')],'https://dreamjob.ru/employers/example/vakansii');
assert.equal(values[headers.indexOf('Эйч')],'https://h.careers/companies/example');
console.log('AI rating and company export verified: 100-point total, 0/5/10 awards, WLB migration, DreamJob and Эйч fields');
