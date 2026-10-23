import {prepareBackend,moduleFor,render,shell,user as sessionUser,nav} from './test-support.mjs';
await prepareBackend();
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const worker=(await import('../.tests/backend.mjs')).default;
import { buildBackupSchema } from './backup-schema.mjs';
const env={SITE_PASSWORD:'synthetic-admin-password',NASTYA_PASSWORD:'synthetic-user-password',DEMO_PASSWORD:'synthetic-demo-password',SESSION_SECRET:'synthetic-documentation-test-secret'};
const origin='https://documentation-test.example';
async function login(name,password){const response=await worker.fetch(new Request(origin+'/auth',{method:'POST',body:new URLSearchParams({login:name,password})}),env);assert.equal(response.status,303);return response.headers.get('set-cookie').split(';')[0]}
const admin=await login('Eugene',env.SITE_PASSWORD),user=await login('Nastya',env.NASTYA_PASSWORD),demo=await login('Demo',env.DEMO_PASSWORD);
const request=(path,cookie,method='GET',activeEnv=env)=>worker.fetch(new Request(origin+path,{method,headers:cookie?{cookie}:undefined}),activeEnv);
for(const path of ['/api/docs','/api/docs/user','/api/docs/database?download=1','/api/docs/missing']){
 assert.equal((await request(path)).status,401);
 for(const cookie of [user,demo])assert.equal((await request(path,cookie)).status,403);
}
const index=await (await request('/api/docs',admin)).json();assert.equal(index.sections.length,8);assert.equal(index.versions.backup,39);
const dbForbidden={...env,DB:new Proxy({},{get(){throw new Error('Documentation must not access the DB')}})};
const markdowns=[];
for(const section of index.sections){
 const response=await request('/api/docs/'+section.id,admin,'GET',dbForbidden);assert.equal(response.status,200);assert.match(response.headers.get('content-type'),/^text\/markdown/);assert.match(response.headers.get('cache-control'),/no-store/);
 const markdown=await response.text();assert.ok(markdown.startsWith('# '));assert.ok(markdown.length>1000);assert.ok(!/\{\{[A-Z_]+\}\}/.test(markdown));markdowns.push(markdown);
 assert.equal(markdown,fs.readFileSync(new URL('../docs/'+section.file,import.meta.url),'utf8'));
 const download=await request('/api/docs/'+section.id+'?download=1',admin);assert.match(download.headers.get('content-disposition'),/^attachment; filename="[A-Z_]+\.md"$/);
 for(const method of ['POST','PUT','DELETE'])assert.equal((await request('/api/docs/'+section.id,admin,method)).status,405);
}
assert.equal((await request('/api/docs/not-a-section',admin)).status,404);
assert.equal((await request('/api/docs/%2e%2e%2fDATABASE.md',admin)).status,404);
const {default:Settings}=await moduleFor('frontend/features/settings/Settings.tsx');
assert.match(render(Settings,{user:sessionUser(true),tab:'docs'}),/Настройки · Документация/);
assert.ok(!render(Settings,{user:sessionUser(false),tab:'docs'}).includes('Документация'));
const {settingsSections}=await moduleFor('frontend/features/settings/sections.ts');
assert.ok(settingsSections(true).some(([key])=>key==='docs'));
assert.deepEqual(settingsSections(false).map(([key])=>key),['general','resume']);
const {Markdown}=await moduleFor('frontend/components/Markdown.tsx');
for(const markdown of markdowns){const html=render(Markdown,{value:markdown});assert.match(html,/<h1>/);assert.ok(!html.includes('&lt;!-- AUTO:'));assert.ok(!html.includes('<script>'));}
const rendered=render(Markdown,{value:'# Test\n\n| A | B |\n| --- | --- |\n| 1 | 2 |\n\n```js\n<script>alert(1)</script>\n```\n\n[Bad](javascript:alert)\n<img src=x onerror=alert(1)>'});
assert.match(rendered,/<table>/);assert.match(rendered,/<pre><code>/);assert.ok(!rendered.includes('<script>'));assert.ok(!rendered.includes('<img '));assert.ok(!rendered.includes('href="javascript:'));
assert.match(render(Markdown,{value:'[API](API.md)'}),/href="API.md"/);
const database=markdowns[index.sections.findIndex(s=>s.id==='database')];for(const t of buildBackupSchema(process.cwd()).tables){assert.ok(database.includes('## '+t.name));for(const c of t.columns)assert.ok(database.includes('| '+c.name+' |'))}
console.log('Documentation verified: all 8 sections, admin-only HTML/API/downloads, no DB access, read-only methods, Markdown escaping/tables/code/links and all SQL fields.');
