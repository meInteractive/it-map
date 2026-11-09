import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import {mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import path from 'node:path';
import ts from 'typescript';

const root=process.cwd();
const webConfigPath=path.join(root,'tsconfig.web.json');
const config=ts.readConfigFile(webConfigPath,ts.sys.readFile);
assert.equal(config.error,undefined,'tsconfig.web.json must be valid');
const parsed=ts.parseJsonConfigFileContent(config.config,ts.sys,root,undefined,webConfigPath);
assert.equal(parsed.errors.length,0,'SPA TypeScript options must parse');
assert.equal(parsed.options.strict,true,'strict mode must remain on');
assert.equal(parsed.options.noEmit,true,'typecheck must never emit files');
assert.equal(parsed.options.skipLibCheck,true,'keep dependency checks as in base config');
const included=new Set(parsed.fileNames.map(name=>path.relative(root,name).split(path.sep).join('/')));
const sources=[];
const crawl=dir=>{
 for(const entry of readdirSync(path.join(root,dir),{withFileTypes:true})){
  const child=dir+'/'+entry.name;
  if(entry.isDirectory())crawl(child);
  else if(/\.(ts|tsx|d\.mts)$/.test(child))sources.push(child);
 }
};
crawl('frontend');
crawl('shared');
for(const name of [...sources,'web/main.tsx','vite.web.config.ts'])
 assert.ok(included.has(name),'Missing active SPA TypeScript source: '+name);
assert.ok(sources.some(name=>name.includes('/features/settings/')));
assert.ok(!included.has('app/page.tsx'),'Legacy Next app should not be a root in SPA config');
const packageScripts=JSON.parse(readFileSync(path.join(root,'package.json'),'utf8')).scripts;
assert.equal(packageScripts['typecheck:web'],'tsc --project tsconfig.web.json --noEmit');
const flow=readFileSync(path.join(root,'.github/workflows/go-ci.yml'),'utf8');
const name='GO-113 — обязательная строгая проверка типов React/Vite';
const gate=flow.indexOf(name),ciInstall=flow.indexOf('npm ci --no-audit --no-fund',flow.indexOf('  web:')),build=flow.indexOf('npm run build:web',flow.indexOf('  web:'));
assert.ok(ciInstall>=0&&gate>ciInstall&&build>gate,'TypeScript gate must run after npm ci and before Vite build');
assert.ok(flow.slice(gate,gate+200).includes('npm run typecheck:web'),'CI must run independent tsc check');
assert.ok(!flow.slice(gate,gate+200).includes('|| true'),'CI must not swallow TypeScript errors');

// One negative and one positive real compiler invocation. No files in the repo
// and no production data: the test project only uses a disposable temp folder.
const temporary=mkdtempSync(path.join(tmpdir(),'itmap-go113-typecheck-'));
try{
 writeFileSync(path.join(temporary,'tsconfig.json'),JSON.stringify({
  compilerOptions:{strict:true,noEmit:true,skipLibCheck:true,types:[],target:'ES2017'},
  files:['sentinel.ts']
 }));
 const sentinel=path.join(temporary,'sentinel.ts');
 const compiler=path.join(root,'node_modules/typescript/bin/tsc');
 const run=()=>spawnSync(process.execPath,[compiler,'--project',temporary],
  {encoding:'utf8',timeout:30000,maxBuffer:1024*1024});
 writeFileSync(sentinel,'const impossible: string = 42;\n');
 let result=run();
 assert.notEqual(result.status,0,'Known type mismatch must fail tsc');
 assert.match((result.stdout||'')+(result.stderr||''),/TS2322/);
 writeFileSync(sentinel,'const possible: string = "ok";\n');
 result=run();
 assert.equal(result.status,0,'Corrected assignment must pass tsc');
}finally{
 rmSync(temporary,{recursive:true,force:true});
}
console.log('GO-113: '+sources.length+' SPA/shared TypeScript sources included; strict/noEmit gate and negative/positive compiler sentinel passed.');
