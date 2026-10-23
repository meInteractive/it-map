import fs from 'node:fs';import {spawnSync} from 'node:child_process';
fs.mkdirSync('.tests',{recursive:true});
const names=fs.readdirSync('scripts').filter(n=>/^test-.*\.mjs$/.test(n)&&n!=='test-support.mjs').sort();
names.splice(names.indexOf('test-backup-roundtrip.mjs'),1);names.unshift('test-backup-roundtrip.mjs');
const results=[];for(const name of names){const args=['scripts/'+name];if(name==='test-backup-roundtrip.mjs'||name==='test-backup-miniflare.mjs')args.push('.tests/backup-fixture.json');const r=spawnSync(process.execPath,args,{encoding:'utf8',timeout:90000,maxBuffer:16*1024*1024});fs.writeFileSync('.tests/'+name+'.log',(r.stdout||'')+(r.stderr||''));results.push({name,passed:r.status===0});console.log((r.status===0?'PASS ':'FAIL ')+name);if(r.status!==0)console.log((r.stderr||r.stdout||String(r.error)).slice(-2000));}fs.writeFileSync('.tests/results.json',JSON.stringify(results,null,2));process.exitCode=results.every(r=>r.passed)?0:1;
