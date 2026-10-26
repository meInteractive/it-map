// The unchanged legacy pure functions are the oracle; no SQL/network/provider.
import {readFile,writeFile} from 'node:fs/promises';
globalThis.fetch=()=>{throw new Error('Channel contract oracle must not use the network');};
const {telegramChannelInput,normalizeTelegramUsername}=await import('../server/repositories/channels.js');
const base={name:'CI synthetic channel',url:'@ci_synthetic',comment:'',lastSuccessfulCheckAt:null};
const current={...base,url:'https://t.me/ci_existing',normalizedUsername:'ci_existing',username:'@ci_existing',comment:'CI old comment',lastSuccessfulCheckAt:'2026-10-26T11:45:00.000Z'};
const vectors=[];
const add=(name,input,current=null)=>{try{vectors.push({name,input,current,valid:true,normalized:telegramChannelInput(input,current)})}catch(error){vectors.push({name,input,current,valid:false,error:error.message})}};
for(const [name,patch]of[
 ['base',{}],['trim-name',{name:' \uFEFF CI synthetic channel \uFEFF '}],['nel-name',{name:'\u0085CI synthetic\u0085'}],['empty-name',{name:''}],
 ['name-limit',{name:'x'.repeat(160)}],['name-over-limit',{name:'x'.repeat(161)}],['name-emoji-limit',{name:'😀'.repeat(80)}],['name-emoji-over-limit',{name:'😀'.repeat(80)+'x'}],
 ['empty-comment',{comment:''}],['comment-untrimmed',{comment:' \uFEFF CI synthetic \uFEFF '}],['comment-limit',{comment:'x'.repeat(512)}],['comment-over-limit',{comment:'x'.repeat(513)}],['comment-emoji-limit',{comment:'😀'.repeat(256)}],['comment-emoji-over-limit',{comment:'😀'.repeat(257)}],
 ['utc-check',{lastSuccessfulCheckAt:'2026-10-26T11:45:00Z'}],['msk-check',{lastSuccessfulCheckAt:'2026-10-26T00:15:00+03:00'}],['fraction-check',{lastSuccessfulCheckAt:'2026-10-26T11:45:00.123456Z'}],['trim-check',{lastSuccessfulCheckAt:'\uFEFF2026-10-26T11:45:00Z\uFEFF'}],['blank-check',{lastSuccessfulCheckAt:' '}],['bad-check',{lastSuccessfulCheckAt:'CI invalid date'}],['bad-offset-hour',{lastSuccessfulCheckAt:'2026-10-26T11:45:00+24:00'}],['bad-offset-minute',{lastSuccessfulCheckAt:'2026-10-26T11:45:00+00:60'}],['bad-comma-fraction',{lastSuccessfulCheckAt:'2026-10-26T11:45:00,123Z'}],
])add(name,{...base,...patch});
const usernames=['@CI_SYNTHETIC','t.me/ci_synthetic','https://t.me/ci_synthetic','HTTP://WWW.T.ME/S/CI_SYNTHETIC/123?x=1#part','https://telegram.me/ci_synthetic','@12345','@_____', '@'+ 'x'.repeat(64),'@'+ 'x'.repeat(65),'@abcd','@ci-synthetic','ci_synthetic','https://example.org/ci_synthetic','https://t.me','https://t.me/s','https://t.me/+ci_synthetic','@@ci_synthetic','https://t.me/@ci_synthetic','https://t.me/%63i_synthetic','https://t.me//s///ci_synthetic/123','https://t.me:443/ci_synthetic','https://www.telegram.me/ci_synthetic','\uFEFF@ci_synthetic\uFEFF','\u0085@ci_synthetic\u0085'];
for(const [i,url]of usernames.entries())add('username-'+i,{...base,url});
add('username-alias',{name:base.name,username:'@ci_alias'});
add('url-wins-over-alias',{...base,username:'@ci_alias'});
add('empty-url-does-not-fallback',{...base,url:'',username:'@ci_alias'});
add('update-empty',{},current);add('update-comment',{comment:'CI new comment'},current);add('update-null-fallback',{name:null,url:null,comment:null,lastSuccessfulCheckAt:null},current);add('update-clear-check',{lastSuccessfulCheckAt:''},current);add('update-username-alias',{username:'@ci_alias'},current);
const result=JSON.stringify({oracle:'Unchanged legacy telegramChannelInput/normalizeTelegramUsername; typed synthetic UI payloads and RFC3339 dates only; no SQL/network',vectors,usernames:usernames.map(input=>({input,output:normalizeTelegramUsername(input)}))},null,2)+'\n';
const target=new URL('../internal/channels/testdata/input-contract.json',import.meta.url);
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,result);
else if(process.argv.length!==2)throw new Error('Use --write only for reviewed fixture regeneration');
else if(await readFile(target,'utf8')!==result)throw new Error('Legacy channel contract differs from committed vectors; review before regenerating');
console.log(`PASS: ${vectors.length} channel input and ${usernames.length} username vectors from unchanged JS; no SQL/network`);
