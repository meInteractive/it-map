// Required source-only contact validator oracle. Synthetic values; no SQL/network.
import assert from 'node:assert/strict';
import {hiringContactInput} from '../server/repositories/contacts.js';
globalThis.fetch=()=>{throw new Error('Contact oracle forbids network');};
const vectors=[];
function add(name,input,currentCompany='CI Contact Employer'){let expected=null,error='';try{expected=hiringContactInput(input,currentCompany)}catch(e){error=e.message}vectors.push({name,input,currentCompany,expected,error});}
const base={position:1,fullName:' CI Contact ',linkedinUrl:'https://example.invalid/ci',email:'ci@example.invalid'};
add('typed contact',base);add('minimal',{position:3,fullName:'CI Contact'});
for(const position of [null,0,1,2,3,4,-1,1.5,'2','0x2',true,false,[],[2],{},'Infinity','',9007199254740992])add('position '+JSON.stringify(position),{...base,position});
for(const field of ['fullName','linkedinUrl','facebookUrl','setkaUrl','telegram','phone','email','specialization']){
 for(const value of [undefined,null,false,0,'',true,23,[],['CI','Contact'],{},'\ufeff CI \ufeff','\u0085'])add(field+' value '+JSON.stringify(value),{...base,[field]:value});
}
for(const [field,limit]of [['fullName',256],['telegram',128],['phone',64],['specialization',512]])for(const [value,name]of [['x'.repeat(limit),'boundary'],['x'.repeat(limit+1),'over'],['😀'.repeat(limit/2),'UTF16 boundary'],['😀'.repeat(limit/2+1),'UTF16 over']])add(field+' '+name,{...base,[field]:value});
const urls=['https://example.invalid/x','HTTP://example.invalid:80','http:example.invalid','https:/example.invalid','https:\\example.invalid\\x','https://user:ci@example.invalid','https://пример.рф/путь','https://😀.invalid','https://127.1','http://0x7f000001','http://0177.0.0.1','http://[::1]/','http://[::1','http://[fe80::1%25eth0]','https://example.invalid:65535','https://example.invalid:65536','https://example.invalid:abc','https://','//example.invalid','example.invalid','mailto:ci@example.invalid','javascript:alert(1)','https://exa mple.invalid','https://example.invalid/%bad','https://%65xample.invalid','https://%xx.invalid','https://a\tb.invalid','https://example.invalid/\nci','https://example.invalid/\u0000ci','https://example.invalid/😀','http://09','http://1.2.3.999','https://\u0085.invalid','https://\u200D.invalid','https://\u{1E4D0}.invalid'];
for(const field of ['linkedinUrl','facebookUrl','setkaUrl'])for(const value of urls)add(field+' URL '+JSON.stringify(value),{...base,[field]:value});
for(const email of ['ci@example.invalid','ci+x@sub.example.invalid','a@b.c','a@@b.c','a@b','a b@c.d','a\u0085b@c.d','a\ufeffb@c.d','@b.c','a@.c','a@b.','a@b.c.d','x'.repeat(242)+'@ci.invalid','x'.repeat(243)+'@ci.invalid'])add('email '+email,{...base,email});
add('company fallback',{...base,companyName:' CI fallback '},'');add('company override',{...base,companyName:'CI ignored'},' CI owner ');
for(const field of ['linkedinUrl','facebookUrl','setkaUrl']){
 const prefix='https://example.invalid/';for(const [name,suffix]of [['boundary','x'.repeat(2048-prefix.length)],['over','x'.repeat(2049-prefix.length)],['UTF16 boundary','😀'.repeat(1012)],['UTF16 over','😀'.repeat(1013)]])add(field+' URL length '+name,{...base,[field]:prefix+suffix});
}
for(const email of ['a@.b.c','a@b.c.','a@b..','a@..b'])add('email unusual '+email,{...base,email});
for(const field of ['fullName','linkedinUrl','facebookUrl','setkaUrl','telegram','phone','email','specialization','position'])for(const toString of [null,7])add(field+' non-callable toString '+toString,{...base,[field]:{toString}});
for(const companyName of [undefined,null,false,0,2,true,[],[' CI ','fallback'],{},{toString:null}])add('company coercion '+JSON.stringify(companyName),{...base,companyName},'');
assert.equal(vectors.length,297);

if(process.argv.includes('--stdio'))for(const v of vectors)process.stdout.write(JSON.stringify(v)+'\n');else console.log(`PASS: ${vectors.length} unchanged contact input vectors, no SQL/network`);
