// Capabilities checked against official OpenAI model pages on 2026-10-08.
// /v1/models supplies availability, not reasoning capabilities.
import {openaiPricing} from './openai-pricing.js';
export const REQUESTED_OPENAI_DEFAULT='gpt-6.1-luna';
export const OPENAI_RECOMMENDED=['gpt-6-luna','gpt-5.6-terra','gpt-6.1-sol','gpt-6-sol','gpt-5.6-sol'];
const efforts=['none','low','medium','high','xhigh','max'];
const profiles={
 'gpt-6-luna':{name:'GPT-6 Luna',efforts},
 'gpt-5.6-terra':{name:'GPT-5.6 Terra',efforts},
 'gpt-6.1-sol':{name:'GPT-6.1 Sol',efforts:efforts.slice(1)},
 'gpt-6-sol':{name:'GPT-6 Sol',efforts},
 'gpt-5.6-sol':{name:'GPT-5.6 Sol',efforts},
 'gpt-5.6-luna':{name:'GPT-5.6 Luna',efforts},
 'gpt-6-astra':{name:'GPT-6 Astra',efforts:efforts.slice(1)},
};
export function openaiModelProfile(id){
 const base=Object.keys(profiles).find(base=>id===base||new RegExp('^'+base.replaceAll('.','\\.')+'-\\d{4}-\\d{2}-\\d{2}$').test(id));
 return {id,name:profiles[base]?.name||id,reasoningEfforts:profiles[base]?.efforts||[],recommended:OPENAI_RECOMMENDED.includes(id)||id===REQUESTED_OPENAI_DEFAULT,pricing:openaiPricing(id)};
}
export function openaiTextModel(id){
 return typeof id==='string'&&id.length<=160&&/^(?:gpt-(?:[56](?:[.-]|$)|4\.1(?:-|$)|4o(?:-|$))|chatgpt-[56](?:[.-]|$)|o[134](?:-|$)|ft:(?:gpt-(?:[56]|4\.1|4o)|o[134]))/.test(id)&&!/(?:audio|realtime|transcrib|tts|image|search|deep-research|computer-use|instruct|moderation|embedding)/i.test(id);
}
export function openaiDefault(models){return [REQUESTED_OPENAI_DEFAULT,...OPENAI_RECOMMENDED].find(id=>models.some(m=>m.id===id))||models[0]?.id||'';}
export function validateOpenaiChoice(model,effort){
 if(model&&!openaiTextModel(model))throw Object.assign(new Error('Выберите текстовую модель OpenAI.'),{status:400});
 if(effort!=='auto'&&model&&!openaiModelProfile(model).reasoningEfforts.includes(effort))throw Object.assign(new Error('Этот уровень рассуждений не подтверждён для выбранной модели. Выберите «По умолчанию модели».'),{status:400});
}
