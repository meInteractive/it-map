import {safeListingUrl} from '../server/integrations/firecrawl-transport.js';
import {extractionUsage} from '../server/services/vacancy-extraction.js';
const urls=['https://example.com/ci','http://example.com/ci','https://EXAMPLE.COM:443/v','https://пример.рф/v','https://127.0.0.1/v','https://[::1]/v','https://10.0.0.1/v','https://localhost/v','https://abc.internal/v','https://abc.test/v','https://example.com:444/v','https://user:pass@example.com/v','https://example.com/a\nother','javascript:alert(1)','https://example.com/ok#fragment'];
const urlFrames=urls.map(url=>{try{safeListingUrl(url);return {url,ok:true}}catch{return {url,ok:false}}});
const cases=[
 {provider:'openai',model:'gpt-6-luna',usage:{input_tokens:10,output_tokens:2}},
 {provider:'openai',model:'gpt-6-luna',usage:{input_tokens:10,output_tokens:2,input_tokens_details:{cached_tokens:3}}},
 {provider:'openai',model:'unknown',usage:{input_tokens:10,output_tokens:2}},
 {provider:'openai',model:'gpt-6-luna',usage:{input_tokens:10,output_tokens:2,cost:0.007}},
 {provider:'openrouter',model:'vendor/free',usage:{prompt_tokens:10,completion_tokens:2,cost:0}},
 {provider:'openrouter',model:'vendor/free',usage:{prompt_tokens:10,completion_tokens:2}},
 {provider:'openai',model:'gpt-6-luna',usage:{input_tokens:-1,output_tokens:2}},
 {provider:'openai',model:'gpt-6-luna',usage:{}},
];
console.log(JSON.stringify({urlFrames,usageFrames:cases.map(c=>({...c,result:extractionUsage({model:c.model,usage:c.usage},c.provider)}))}));
