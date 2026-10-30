// Только pure source functions: никакой сети/БД/ключей.
import {readFileSync} from 'node:fs';
import {openaiModelProfile,openaiTextModel,openaiDefault,OPENAI_RECOMMENDED,REQUESTED_OPENAI_DEFAULT} from '../server/integrations/openai-models.js';
import {orRankModels} from '../server/integrations/openrouter.js';
const pricing=readFileSync(new URL('../server/integrations/openai-pricing.js',import.meta.url),'utf8');
const ids=[...new Set([...OPENAI_RECOMMENDED,REQUESTED_OPENAI_DEFAULT,...[...pricing.matchAll(/^ '([^']+)':\[/gm)].map(m=>m[1]),'gpt-5.6'])];
if(process.argv.includes('--profiles')){console.log(JSON.stringify(Object.fromEntries(ids.map(id=>[id,openaiModelProfile(id)]))));process.exit(0);}
const aiids=[...ids,...ids.map(id=>id+'-2026-10-09'),'gpt-5-custom','gpt-5-audio','gpt-5-embedding','gpt-4.1-mini','o3','chatgpt-5-test','ft:gpt-5:x','gpt-5-😀','random','gpt-5-'+ 'x'.repeat(160)];
const openai=aiids.map(id=>({id,eligible:openaiTextModel(id),profile:openaiModelProfile(id)}));
const make=(id,changes={})=>({id,name:id,context_length:262144,supported_parameters:['reasoning','structured_outputs'],architecture:{output_modalities:['text']},pricing:{prompt:'0',completion:'0'},...changes});
const models=[make('vendor/a'),make('vendor/B'),make('vendor/b'),make('vendor/a'),make('openrouter/free'),make('vendor/c',{context_length:'0x80000'}),make('vendor/d',{context_length:262143}),make('vendor/e',{pricing:{prompt:'0.1',completion:'0'}}),make('vendor/f',{pricing:{prompt:'',completion:'0'}}),make('vendor/g',{supported_parameters:[]}),make('vendor/h',{architecture:{output_modalities:['image']}}),make('vendor/i:free',{canonical_slug:'vendor/i',pricing:{prompt:0,completion:0,request:'0x0'}})];
const benchmarks=[{source:'artificial-analysis',model_permaslug:'vendor/a',intelligence_index:'5',agentic_index:1,coding_index:null},{source:'artificial-analysis',model_permaslug:'vendor/c',intelligence_index:'0xA',agentic_index:' ',coding_index:3},{source:'artificial-analysis',model_permaslug:'vendor/i',intelligence_index:5,agentic_index:2,coding_index:2}];
console.log(JSON.stringify({openai,defaultModel:openaiDefault(aiids.filter(openaiTextModel).map(openaiModelProfile)),router:{models,benchmarks,at:'2026-10-30T00:00:00.000Z',expected:orRankModels(models,benchmarks,'2026-10-30T00:00:00.000Z')}}));
