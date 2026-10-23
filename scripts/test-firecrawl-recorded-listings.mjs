import assert from 'node:assert/strict';import fs from 'node:fs';import {extractVacancyListing} from '../server/services/vacancy-listing-extractor.js';
const {fixtures}=JSON.parse(fs.readFileSync('scripts/fixtures/firecrawl-listings.json'));
for(const f of fixtures){const x=extractVacancyListing(f.data,{pageUrl:f.pageUrl,hosts:[new URL(f.pageUrl).hostname],config:{adapter:f.adapter}});assert.equal(x.jobs.length,f.expected,f.company);assert.ok(x.jobs.every(j=>j.title!=='Москва'&&!j.title.includes('Санкт-Петербург')));}
console.log('Recorded real Firecrawl listings: Selectel bold-card pairing/no city titles, Ozon/Habr, Cian CAPTCHA and MIND form produce expected pairs; zero live CI requests');
