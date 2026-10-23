import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
import {buildBackupSchema} from './backup-schema.mjs';
const config=JSON.parse(fs.readFileSync('dist/server/wrangler.json','utf8'));
const options={cf:false,modules:['index.js',...fs.readdirSync('dist/server',{recursive:true}).filter(f=>/\.m?js$/.test(f)&&f!=='index.js')].map(f=>({type:'ESModule',path:path.resolve('dist/server',f)})),modulesRoot:path.resolve('dist/server'),compatibilityDate:config.compatibility_date,compatibilityFlags:config.compatibility_flags,d1Databases:{DB:'runtime-synthetic'},bindings:{SITE_PASSWORD:'synthetic',NASTYA_PASSWORD:'synthetic',SESSION_SECRET:'runtime-synthetic-secret'},assets:{directory:path.resolve('dist/client'),binding:'ASSETS',routerConfig:{has_user_worker:true}},outboundService:()=>new Response('External network disabled',{status:503})};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB');await db.batch(buildBackupSchema(process.cwd()).objects.map(o=>db.prepare(o.sql)));
 const req=(url,options={})=>mf.dispatchFetch('https://runtime.test'+url,options);
 let response=await req('/');let html=await response.text();assert.equal(response.status,200,html.slice(0,500));assert.match(html,/Загрузка/);assert.ok(!html.includes('DATA.companies'));assert.ok(!html.includes('job@yoomoney.ru'));assert.match(response.headers.get('content-security-policy'),/worker-src 'self' blob:/);
 const assets=[...html.matchAll(/(?:src|href)="(\/_next\/[^"?]+)/g)].map(m=>m[1]);assert.ok(assets.length>0);for(const url of new Set(assets)){const r=await req(url);assert.equal(r.status,200,url);assert.ok(!(await r.text()).startsWith('<!DOCTYPE'));}
 assert.equal((await req('/api/session')).status,401);
 response=await req('/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'synthetic'}),redirect:'manual'});assert.equal(response.status,303);const cookie=response.headers.get('set-cookie').split(';')[0];assert.match(response.headers.get('set-cookie'),/HttpOnly/);
 const session=await req('/api/session',{headers:{cookie}});assert.equal(session.status,200);assert.equal((await session.json()).isAdmin,true);
 response=await req('/',{headers:{cookie}});assert.equal(response.status,200);html=await response.text();assert.ok(!html.includes('DATA.companies'));assert.ok(!html.includes('job@yoomoney.ru'));
 response=await req('/auth',{method:'POST',body:new URLSearchParams({login:'Eugene',password:'wrong'}),redirect:'manual'});assert.equal(response.status,401);assert.match(await response.text(),/Загрузка/);
 for(const url of ['/manifest.webmanifest','/service-worker.js','/icons/icon-192.png','/icons/icon-512.png'])assert.equal((await req(url)).status,200,url);
 const manifest=await (await req('/manifest.webmanifest')).json();assert.equal(manifest.display,'standalone');
 const list=await req('/api/companies/list',{headers:{cookie}});assert.equal(list.status,200,await list.clone().text());assert.equal((await list.json()).companies.length,200);
 console.log('Production Worker: React shell, auth/session, built JS/CSS, API/D1 and PWA assets passed in workerd.');
}finally{await mf.dispose();}
