// Build-time only private ICU ABI probe. Never loaded by the application.
import assert from 'node:assert/strict';
import {mkdtempSync,openSync,closeSync,readFileSync,copyFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {execFileSync} from 'node:child_process';
for(const [k,v]of Object.entries({node:'24.19.0',icu:'78.3',cldr:'48.0',unicode:'17.0'}))assert.equal(process.versions[k],v,`Pinned ${k} required`);
assert.equal(process.platform,'linux');assert.equal(process.arch,'x64');
const root=fileURLToPath(new URL('./',import.meta.url)),out=fileURLToPath(new URL('../../internal/rucollation/weights.json',import.meta.url));
const dir=mkdtempSync(join(tmpdir(),'itmap-public-collation-'));
try {
 const addon=join(dir,'generate.node'),native=join(dir,'native.ndjson'),plain=join(dir,'policy.json'),packed=join(dir,'packed.json');
 execFileSync('c++',['-std=c++17','-shared','-fPIC','-O2',join(root,'generate.cc'),'-ldl','-o',addon]);
 const fd=openSync(native,'w');try {execFileSync(process.execPath,['-e','require(process.argv[1])',addon],{stdio:['ignore',fd,'inherit']});}finally{closeSync(fd)}
 execFileSync(process.execPath,[join(root,'compress.mjs'),native,plain],{stdio:'inherit'});
 execFileSync(process.execPath,[join(root,'pack.mjs'),plain,packed],{stdio:'inherit'});
 if(process.argv.includes('--write'))copyFileSync(packed,out);else assert.deepEqual(readFileSync(packed),readFileSync(out),'Pinned collation data drift');
 console.log('PASS: complete Unicode17/ICU78.3 table reproduced; no application/native dependency');
}finally{rmSync(dir,{recursive:true,force:true})}
