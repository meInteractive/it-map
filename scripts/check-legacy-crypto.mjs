// SYNTHETIC compatibility vectors only. Never reads env, keys, DB or providers.
import {readFile,writeFile} from 'node:fs/promises';
import {webcrypto} from 'node:crypto';
globalThis.fetch=()=>{throw new Error('Crypto oracle forbids network');};
const {encryptGoogleToken,decryptGoogleToken}=await import('../server/integrations/google-calendar.js');
const domains=['',':openai:v1',':openrouter:v1',':firecrawl:v1',':apify:v1'];
// Fail if provider derivation changes rather than certifying stale constants.
for(const [file,domain]of [['../server/integrations/openai.js',domains[1]],['../server/integrations/openrouter.js',domains[2]],['../server/repositories/firecrawl-settings.js',domains[3]],['../server/repositories/apify-settings.js',domains[4]]]){
 const source=await readFile(new URL(file,import.meta.url),'utf8');
 if(source.split(`env.SESSION_SECRET+'${domain}'`).length<3)throw new Error('Legacy provider derivation changed; review crypto contract');
}
if(process.argv[2]==='--verify-go'&&process.argv.length===3){
 let input='';for await(const chunk of process.stdin){input+=chunk;if(input.length>1000000)throw new Error('Synthetic verification input too large');}
 const rows=JSON.parse(input);if(!Array.isArray(rows)||rows.length<10)throw new Error('Required Go envelope vectors missing');
 for(const row of rows){
  if(!row.secret.startsWith('CI SYNTHETIC ')||!domains.includes(row.domain))throw new Error('Only synthetic crypto context allowed');
  const plain=await decryptGoogleToken(row.secret+row.domain,row.envelope);
  if(plain!==row.plaintext)throw new Error('Go envelope does not match legacy decryption');
 }
 console.log(`PASS: ${rows.length} Go random-nonce envelopes decrypted by unchanged legacy JS`);
}else{
 if(process.argv.length>3||(process.argv[2]&&process.argv[2]!=='--write'))throw new Error('Use --write or --verify-go only for reviewed synthetic fixtures');
 const original=globalThis.crypto;
 const vectors=[];
 try{
  for(const domain of domains)for(const [kind,plaintext]of [['ascii','CI SYNTHETIC VALUE'],['unicode','CI синтетика 😀\n\u0000<&>'],['empty','']]){
   const secret=kind==='unicode'?'CI SYNTHETIC секрет 😀':'CI SYNTHETIC session secret, never a credential';
   const iv=Uint8Array.from({length:12},(_,i)=>(i+vectors.length*13)%256);
   Object.defineProperty(globalThis,'crypto',{configurable:true,value:{subtle:webcrypto.subtle,getRandomValues(target){if(target.length!==12)throw new Error('Legacy nonce contract changed');target.set(iv);return target;}}});
   const envelope=await encryptGoogleToken(secret+domain,plaintext);
   if(await decryptGoogleToken(secret+domain,envelope)!==plaintext)throw new Error('Legacy synthetic round trip failed');
   vectors.push({name:(domain||'calendar')+'-'+kind,secret,domain,plaintext,iv:Buffer.from(iv).toString('hex'),envelope});
  }
 }finally{Object.defineProperty(globalThis,'crypto',{configurable:true,value:original});}
 const output=JSON.stringify({synthetic:true,oracle:'Unchanged legacy AES-GCM helpers; known synthetic secret/plaintext/nonce; no real keys, DB or network',vectors},null,2)+'\n';
 const target=new URL('../internal/legacycrypto/testdata/compatibility.json',import.meta.url);
 if(process.argv[2]==='--write')await writeFile(target,output);
 else if(await readFile(target,'utf8')!==output)throw new Error('Legacy crypto vectors changed; review before regeneration');
 console.log(`PASS: ${vectors.length} synthetic AES-GCM vectors from unchanged JS`);
}
