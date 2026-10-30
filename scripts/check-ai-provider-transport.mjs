// Только mock fetch: исходные клиенты не получают реальные ключи и сеть.
import {openaiFetch} from '../server/integrations/openai.js';
import {orFetch} from '../server/integrations/openrouter.js';
const original=globalThis.fetch,rows=[];
try {
 for(const provider of ['openai','openrouter']) {
  const cases=[...([200,400,401,403,404,429,500,503].flatMap(status=>[false,true].map(post=>({status,post})))),{status:200,post:false,embedded:401},{status:200,post:true,embedded:429},{status:400,post:true,reasoning:true},{status:200,post:false,malformed:true},{status:429,post:true,malformed:true}];
  for(const c of cases){
   const path=c.post?(provider==='openai'?'responses':'chat/completions'):'models';
   const value=c.status===200&&!c.embedded?{data:[{id:'synthetic-model'}]}:{error:{code:c.embedded||c.status,message:c.reasoning?'reasoning CI_PRIVATE_MARKER':'CI_PRIVATE_MARKER'}};
   const response=c.malformed?'not JSON':JSON.stringify(value);let endpoint,method;
   globalThis.fetch=async(url,options)=>{endpoint=String(url);method=options.method;return new Response(response,{status:c.status});};
   let result;
   try {await (provider==='openai'?openaiFetch({},path,c.post?{model:'synthetic-model'}:undefined,undefined,'CI_SYNTHETIC_TOKEN'):orFetch('CI_SYNTHETIC_TOKEN',path,c.post?{model:'synthetic-model'}:undefined));result={ok:true};}
   catch(e){result={ok:false,code:e.code,status:e.status,message:e.message,providerStatus:e.providerStatus??0};}
   rows.push({provider,...c,httpStatus:c.status,path,response,endpoint,method,...result});
  }
 }
} finally {globalThis.fetch=original;}
console.log(JSON.stringify(rows));
