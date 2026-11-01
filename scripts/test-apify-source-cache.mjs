import assert from 'node:assert/strict';
import {moduleFor} from './test-support.mjs';
const api=await moduleFor('frontend/api/client.ts');
const original=globalThis.fetch;
const list='/api/apify/hh-sources';
let revision=0,calls=0;
globalThis.fetch=async url=>{assert.equal(url,list);calls++;return Response.json({sources:[{companyId:1,url:'https://hh.ru/employer/'+revision,revision}]});};
try{
 for(const mutation of ['/api/crm/favorite','/api/companies','/api/backup/restore','/api/firecrawl/settings','/api/scheduler/settings','/api/firecrawl/sources/1/hh']){
  api.clearCache();const before=await api.load(list);const previousCalls=calls;
  await api.load(list);assert.equal(calls,previousCalls,'Повторное чтение использует кэш');
  revision++;api.invalidateMutation(mutation,'PUT');
  const after=await api.load(list);
  assert.equal(calls,previousCalls+1,mutation+' должен обновить HH-список');
  assert.notEqual(after.sources[0].url,before.sources[0].url);
 }
 console.log('Apify HH cache: favorite, catalog, restore, scope and source mutations refresh the list.');
}finally{api.clearCache();globalThis.fetch=original;}
