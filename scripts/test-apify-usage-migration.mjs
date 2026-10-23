import assert from 'node:assert/strict';
import fs from 'node:fs';
import {buildBackupSchema} from './backup-schema.mjs';
const {Miniflare,convertV4MiniflareOptions}=await import('miniflare');
const options={cf:false,modules:true,script:'export default {fetch(){return new Response("ok")}}',compatibilityDate:'2026-05-15',d1Databases:{DB:'apify-usage-migration'}};
const mf=new Miniflare(convertV4MiniflareOptions?convertV4MiniflareOptions(options):options);
try{
 const db=await mf.getD1Database('DB'),migration=fs.readFileSync('drizzle/0073_apify_account_usage.sql','utf8'),apply=()=>db.batch(migration.replace(/^--.*$/gm,'').split(';').map(s=>s.trim()).filter(Boolean).map(s=>db.prepare(s)));
 await db.batch(buildBackupSchema(process.cwd()).objects.filter(o=>o.tbl_name!=='apify_account_usage').map(o=>db.prepare(o.sql)));
 const snapshot={accountId:'sameAccount',periodStart:'2026-10-01T00:00:00Z',periodEnd:'2026-11-01T00:00:00Z',providerUsedUsd:2,checkedAt:1};
 await db.prepare('INSERT INTO apify_settings(id,free_account_json,starter_account_json) VALUES(1,?,?)').bind(JSON.stringify(snapshot),JSON.stringify({...snapshot,providerUsedUsd:3,checkedAt:2})).run();
 await apply();
 let rows=(await db.prepare('SELECT * FROM apify_account_usage').all()).results;assert.equal(rows.length,1);assert.equal(rows[0].id,JSON.stringify([snapshot.accountId,snapshot.periodStart,snapshot.periodEnd]));assert.equal(rows[0].provider_used_usd,3,'two profiles seed one account high-water');
 await db.prepare('UPDATE apify_account_usage SET provider_used_usd=4').run();await apply();assert.equal((await db.prepare('SELECT provider_used_usd FROM apify_account_usage').first()).provider_used_usd,4,'idempotent migration cannot lower known usage');
 await db.prepare("UPDATE apify_settings SET free_account_json='',starter_account_json='invalid-json'").run();await apply();assert.equal((await db.prepare('SELECT count(*) n FROM apify_account_usage').first()).n,1,'invalid/empty settings are ignored');
 assert.equal((await db.prepare('SELECT count(*) n FROM apify_runs').first()).n,0,'migration never fabricates Actor runs');
 console.log('Native D1 Apify usage migration: account-wide seed from both profiles, no lowering on replay, invalid JSON ignored and no fabricated Actor ledger rows passed.');
}finally{await mf.dispose();}
