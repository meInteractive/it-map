import {ROLE_SETTINGS_SQL} from './vacancy-import-schema.js';
import {DEFAULT_ROLE_PROMPT} from '../services/vacancy-role-prompt.js';
import {DETAIL_SETTINGS_SQL} from './vacancy-detail-schema.js';
import {DEFAULT_EXTRACTION_PROMPT,ALIGN_EXTRACTION_PROMPT_SQL} from '../services/vacancy-extraction-prompt.js';
export async function readDetailSettings(db,kind='vacancy_extraction'){const role=kind==='vacancy_role';await db.prepare(role?ROLE_SETTINGS_SQL:DETAIL_SETTINGS_SQL).run();if(!role)await db.prepare(ALIGN_EXTRACTION_PROMPT_SQL).run();return await db.prepare('SELECT * FROM '+(role?'vacancy_role_settings':'vacancy_detail_settings')+' WHERE id=1').first()||{markdown:role?DEFAULT_ROLE_PROMPT:DEFAULT_EXTRACTION_PROMPT,prompt_revision:0,provider:role?'openrouter':'openai',openrouter_model:role?'jev':'best_free',openai_model:'gpt-6-luna',reasoning_effort:'medium',model_revision:0,updated_at:null};}
export async function saveDetailSettings(db,part,input,kind='vacancy_extraction'){
 const role=kind==='vacancy_role',table=role?'vacancy_role_settings':'vacancy_detail_settings';await db.prepare(role?ROLE_SETTINGS_SQL:DETAIL_SETTINGS_SQL).run();
 await db.prepare('INSERT OR IGNORE INTO '+table+(role?"(id,markdown,provider,openrouter_model) SELECT 1,?,'openrouter','jev' WHERE ?=0":'(id,markdown) SELECT 1,? WHERE ?=0')).bind(role?DEFAULT_ROLE_PROMPT:DEFAULT_EXTRACTION_PROMPT,input.revision).run();
 const row=part==='prompt'?await db.prepare('UPDATE '+table+' SET markdown=?,prompt_revision=prompt_revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=1 AND prompt_revision=? RETURNING *').bind(input.markdown,input.revision).first():await db.prepare('UPDATE '+table+' SET provider=?,openai_model=?,'+(role?'openrouter_model=?,':'')+'reasoning_effort=?,model_revision=model_revision+1,updated_at=CURRENT_TIMESTAMP WHERE id=1 AND model_revision=? RETURNING *').bind(input.provider,input.openaiModel,...(role?[input.openrouterModel??'best_free']:[]),input.reasoningEffort,input.revision).first();
 if(!row)throw Object.assign(new Error('Настройки изменены в другой вкладке. Обновите страницу.'),{status:409});return row;
}
export const detailPrompt=(row,kind='vacancy_extraction')=>({kind,markdown:row.markdown,revision:row.prompt_revision,updatedAt:row.updated_at});
export const detailModel=(row,kind='vacancy_extraction')=>({kind,provider:row.provider,openaiModel:row.openai_model,openrouterModel:kind==='vacancy_role'?(row.openrouter_model??'best_free'):'best_free',reasoningEffort:row.reasoning_effort,revision:row.model_revision,updatedAt:row.updated_at});
