import {readDetailSettings,saveDetailSettings,detailModel} from './vacancy-detail-settings.js';
// Shared admin configuration; migrations own the table. No credentials here.
export const PROMPT_MODEL_DEFAULT = {provider:'openrouter',openaiModel:'',openrouterModel:'best_free',reasoningEffort:'medium',revision:0,updatedAt:null};
export async function readPromptModels(db) {
 const rows=(await db.prepare('SELECT kind,provider,openai_model AS openaiModel,openrouter_model AS openrouterModel,reasoning_effort AS reasoningEffort,revision,updated_at AS updatedAt FROM prompt_model_settings').all()).results||[];
 return {...{vacancy_extraction:detailModel(await readDetailSettings(db)),vacancy_role:detailModel(await readDetailSettings(db,'vacancy_role'),'vacancy_role')},...Object.fromEntries(['cover_letter','relevance'].map(kind=>[kind,rows.find(row=>row.kind===kind)||{kind,...PROMPT_MODEL_DEFAULT}]))};
}
export async function readPromptModelInput(request) {
 if(Number(request.headers.get('content-length')||0)>4096)throw Object.assign(new Error('Запрос слишком большой.'),{status:413});
 const reader=request.body?.getReader();if(!reader)throw new Error('Передайте настройки модели.');
 let bytes=0,text='';const decoder=new TextDecoder();
 try{for(;;){const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>4096){await reader.cancel();throw Object.assign(new Error('Запрос слишком большой.'),{status:413});}text+=decoder.decode(value,{stream:true});}}finally{reader.releaseLock();}
 const input=JSON.parse(text+decoder.decode());
 if(!input||Array.isArray(input)||Object.keys(input).some(k=>!['provider','openaiModel','openrouterModel','reasoningEffort','revision'].includes(k))||!['openrouter','openai'].includes(input.provider)||typeof input.openaiModel!=='string'||(input.openaiModel!==''&&!/^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,159}$/.test(input.openaiModel))||!['auto','none','minimal','low','medium','high','xhigh','max'].includes(input.reasoningEffort)||!Number.isSafeInteger(input.revision)||input.revision<0)throw new Error('Некорректные настройки модели.');
 if(input.openrouterModel!==undefined&&!['best_free','jev'].includes(input.openrouterModel))throw new Error('Некорректная модель OpenRouter.');
 return {...input,openrouterModel:input.openrouterModel??'best_free'};
}
export async function savePromptModel(db,kind,input) {
 if(!['best_free','jev'].includes(input.openrouterModel??'best_free'))throw new Error('Некорректная модель OpenRouter.');
 if(!['relevance','vacancy_role'].includes(kind)&&input.openrouterModel==='jev')throw new Error('JEV доступна для релевантности и определения конечных ролей.');
 if(['vacancy_extraction','vacancy_role'].includes(kind))return detailModel(await saveDetailSettings(db,'model',input,kind),kind);
 const row=await db.prepare(`INSERT INTO prompt_model_settings(kind,provider,openai_model,openrouter_model,reasoning_effort,revision,updated_at)
 SELECT ?,?,?,?,?,1,CURRENT_TIMESTAMP WHERE ?=0 OR EXISTS(SELECT 1 FROM prompt_model_settings WHERE kind=? AND revision=?)
 ON CONFLICT(kind) DO UPDATE SET provider=excluded.provider,openai_model=excluded.openai_model,openrouter_model=excluded.openrouter_model,reasoning_effort=excluded.reasoning_effort,revision=prompt_model_settings.revision+1,updated_at=CURRENT_TIMESTAMP
 WHERE prompt_model_settings.revision=? RETURNING kind,provider,openai_model AS openaiModel,openrouter_model AS openrouterModel,reasoning_effort AS reasoningEffort,revision,updated_at AS updatedAt`)
 .bind(kind,input.provider,input.openaiModel,input.openrouterModel??'best_free',input.reasoningEffort,input.revision,kind,input.revision,input.revision).first();
 if(!row)throw Object.assign(new Error('Настройки модели изменены в другой вкладке. Обновите страницу.'),{status:409});
 return row;
}
