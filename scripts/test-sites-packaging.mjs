import assert from 'node:assert/strict';
import {mkdtemp,mkdir,writeFile,readFile,readdir,rm,access} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {moduleFor} from './test-support.mjs';

const {sites}=await moduleFor('build/sites-vite-plugin.ts');
const fixture=await mkdtemp(join(tmpdir(),'sites-packaging-test-'));
const hosting={project_id:'synthetic-packaging',d1:'DB',r2:null};
const plugin=(root,command='build')=>{const p=sites({mockAuth:false});p.configResolved({root,command});return p;};
async function seed(root,migrations=true){
 await mkdir(join(root,'.openai'),{recursive:true});
 await writeFile(join(root,'.openai/hosting.json'),JSON.stringify(hosting));
 if(migrations){await mkdir(join(root,'drizzle'));for(let i=0;i<64;i++)await writeFile(join(root,'drizzle',i+'.sql'),'-- synthetic migration '+i+'\n');}
}
async function verify(root,migrations=true){
 assert.deepEqual(JSON.parse(await readFile(join(root,'dist/.openai/hosting.json'),'utf8')),hosting);
 if(migrations){assert.equal((await readdir(join(root,'dist/.openai/drizzle'))).length,64);for(let i=0;i<64;i++)assert.equal(await readFile(join(root,'dist/.openai/drizzle',i+'.sql'),'utf8'),'-- synthetic migration '+i+'\n');}
 else await assert.rejects(access(join(root,'dist/.openai/drizzle')),{code:'ENOENT'});
}
try{
 const root=join(fixture,'shared');await seed(root);
 const one=plugin(root);
 await Promise.all(Array.from({length:8},()=>one.closeBundle()));await verify(root);
 const many=Array.from({length:8},()=>plugin(root));
 await Promise.all(many.map(async(p,i)=>{await new Promise(resolve=>setTimeout(resolve,i*3));await p.closeBundle();}));await verify(root);
 // A subsequent package removes stale files; source migrations remain authoritative.
 await writeFile(join(root,'dist/.openai/drizzle/stale.sql'),'-- stale\n');
 await one.closeBundle();await verify(root);
 const other=join(fixture,'independent');await seed(other,false);
 await Promise.all([plugin(root).closeBundle(),plugin(other).closeBundle()]);await verify(root);await verify(other,false);
 const missing=join(fixture,'retry');await mkdir(missing);
 await assert.rejects(plugin(missing).closeBundle(),{code:'ENOENT'});
 await seed(missing,false);await plugin(missing).closeBundle();await verify(missing,false);
 const serve=join(fixture,'serve');await seed(serve,false);await plugin(serve,'serve').closeBundle();
 await assert.rejects(access(join(serve,'dist')),{code:'ENOENT'});
 console.log('Sites packaging: concurrent environments/instances, staggered hooks, exact migration/identity bytes, stale cleanup, independent roots, failure retry and serve no-op passed.');
}finally{await rm(fixture,{recursive:true,force:true});}
