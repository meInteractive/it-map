import fs from 'node:fs';
import path from 'node:path';
import {build} from 'esbuild';
import {createElement} from 'react';
import {renderToStaticMarkup} from 'react-dom/server';
let counter=0,prepared=false;
export async function prepareBackend(){
 if(prepared)return;prepared=true;fs.mkdirSync('.tests',{recursive:true});
 await import('./prepare-server.mjs');
 const options={bundle:true,format:'esm',platform:'node',target:'es2022',logLevel:'silent'};
 await build({...options,stdin:{contents:"export {default} from './server/routes/index.js';export * from './server/public-api.js';",resolveDir:process.cwd()},outfile:'.tests/backend.mjs'});
 await build({...options,entryPoints:['server/routes/index.js'],outfile:'.tests/worker.mjs'});
}
export async function moduleFor(file){fs.mkdirSync('.tests',{recursive:true});const outfile=path.resolve('.tests/module-'+counter+++'.mjs');await build({entryPoints:[file],outfile,bundle:true,format:'esm',platform:'node',packages:'external',jsx:'automatic',logLevel:'silent',plugins:[{name:'css',setup(b){b.onResolve({filter:/\.css$/},()=>({path:'empty',namespace:'css'}));b.onLoad({filter:/.*/,namespace:'css'},()=>({contents:'',loader:'js'}));}}]});return import(outfile);}
export const render=(component,props={})=>renderToStaticMarkup(createElement(component,props));
export async function shell(){return render((await moduleFor('frontend/Application.tsx')).default);}
export const user=(isAdmin=false)=>({name:isAdmin?'Eugene':'Nastya',isAdmin});
export const nav={openCompany(){},openVacancy(){},openChannel(){}};

// Ordinary mutation fixtures opt in to the new contract. Strict concurrency
// regressions deliberately use raw requests and retain their original revision.
export async function vacancyTestPayload(read, path, method, body) {
 if(method==='PUT'&&/^\/api\/vacancies\/\d+$/.test(path)&&body&&!Object.hasOwn(body,'revision')) {
  const response=await read();
  if(response.ok){const data=await response.json();if(data.vacancy?.revision)return {...body,revision:data.vacancy.revision};}
 }
 return body;
}

// Explicit opt-in for ordinary creation fixtures. Negative, race and retry requests
// use the original body directly; this helper never changes a production request.
export async function applicationTestIntent(readContext) {
 const response = await readContext();
 if (!response.ok) throw new Error('Creation fixture context failed: '+response.status);
 const {creationContext} = await response.json();
 if (typeof creationContext !== 'string' || !/^ac1\.[A-Za-z0-9_-]{43}$/.test(creationContext))
  throw new Error('Invalid creation fixture context');
 return {idempotencyKey:crypto.randomUUID(), creationContext};
}
