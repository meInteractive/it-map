// Actual runOpenAI на mock Responses API, без DB/network/реальных ключей.
import {runOpenAI} from '../server/integrations/openai.js';
const original=globalThis.fetch,rows=[];
try{
 const base={status:'completed',model:'gpt-6-luna',output:[{type:'message',role:'assistant',content:[{type:'output_text',text:'готово'}]}],usage:{input_tokens:3,output_tokens:2}};
 const cases=[{}, {status:'incomplete'}, {model:'foreign-model'}, {model:'gpt-6-luna-2026-10-30'}, {output:[]}, {output:[{type:'message',role:'assistant',content:[{type:'refusal',refusal:'CI_REFUSAL'}]}]}, {output:[{type:'message',role:'assistant',content:[{type:'output_text',text:'  '}]}]}, {output:[{type:'message',role:'assistant',content:[{type:'output_text',text:'first'},{type:'output_text',text:'second'}]}]}];
 for(const [i,patch] of cases.entries()){
  const response={...base,...patch},calls=[];
  globalThis.fetch=async(url,opts)=>{if(String(url).endsWith('/models'))return Response.json({data:[{id:'gpt-6-luna'}]});calls.push(JSON.parse(opts.body));return Response.json(response);};
  let expected;const options={task:'connection_test',messages:[{role:'system',content:'CI_SYSTEM'},{role:'user',content:'CI_USER'}],maxTokens:1024};const config={openaiModel:'gpt-6-luna',reasoningEffort:'medium'};
  // Source logger содержит только synthetic metadata; не смешивать его с JSON oracle.
  const log=console.info;console.info=()=>{};
  try{const result=await runOpenAI({OPENAI_API_KEY:'CI_SYNTHETIC_'+i},options,config);expected={ok:true,result};}
  catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message};}
  finally{console.info=log;}
  rows.push({response,payload:calls[0],...expected});
 }
}finally{globalThis.fetch=original;}
console.log(JSON.stringify(rows));
