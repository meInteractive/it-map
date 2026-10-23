import {DEFAULT_ROLE_PROMPT} from '../services/vacancy-role-prompt.js';
import {ALIGN_RELEVANCE_PROMPT_SQL} from '../services/relevance-prompt.js';
import {readDetailSettings,saveDetailSettings,detailPrompt} from './vacancy-detail-settings.js';
import {DEFAULT_EXTRACTION_PROMPT} from '../services/vacancy-extraction-schema.js';
// Server-only; extracted from the v218 Worker.


// Legacy common limit stays available; each kind advertises its actual limit.
export const MAX_PROMPT_LENGTH=5000;
export const PROMPT_MAX_LENGTHS={cover_letter:15000,relevance:5000,vacancy_extraction:10000,vacancy_role:5000};

export const PROMPT_KINDS=['cover_letter','relevance','vacancy_extraction','vacancy_role'];

export const SYSTEM_PROMPTS_TABLE_SQL=`CREATE TABLE IF NOT EXISTS system_prompts (
  kind TEXT PRIMARY KEY NOT NULL,
  markdown TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT system_prompts_kind CHECK(kind IN ('cover_letter','relevance')),
  CONSTRAINT system_prompts_length CHECK(length(markdown) <= CASE WHEN kind = 'cover_letter' THEN 15000 ELSE 5000 END),
  CONSTRAINT system_prompts_revision CHECK(revision >= 0)
)`;

export async function readSystemPrompts(db){
 await db.prepare(SYSTEM_PROMPTS_TABLE_SQL).run();
 await db.prepare(ALIGN_RELEVANCE_PROMPT_SQL).run();
 const rows=(await db.prepare('SELECT kind,markdown,revision,updated_at AS updatedAt FROM system_prompts').all()).results||[];
 const byKind=new Map(rows.map(row=>[row.kind,row]));byKind.set('vacancy_extraction',detailPrompt(await readDetailSettings(db)));byKind.set('vacancy_role',detailPrompt(await readDetailSettings(db,'vacancy_role'),'vacancy_role'));
 return {prompts:Object.fromEntries(PROMPT_KINDS.map(kind=>[kind,byKind.get(kind)||{kind,markdown:'',revision:0,updatedAt:null}])),maxLength:MAX_PROMPT_LENGTH,maxLengths:PROMPT_MAX_LENGTHS,defaults:{vacancy_extraction:DEFAULT_EXTRACTION_PROMPT,vacancy_role:DEFAULT_ROLE_PROMPT}};
}

export async function readPromptInput(request,kind='relevance'){
 const maxLength=kind==='title_classification'?10000:PROMPT_MAX_LENGTHS[kind]||MAX_PROMPT_LENGTH;
 const limit=maxLength>=10000?128*1024:32*1024;
 if(Number(request.headers.get('content-length')||0)>limit)throw Object.assign(new Error('Запрос слишком большой.'),{status:413});
 if(!request.body)throw new Error('Передайте текст промта.');
 const reader=request.body.getReader(),decoder=new TextDecoder();let bytes=0,text='';
 try{
  for(;;){const {done,value}=await reader.read();if(done)break;bytes+=value.byteLength;if(bytes>limit){await reader.cancel();throw Object.assign(new Error('Запрос слишком большой.'),{status:413});}text+=decoder.decode(value,{stream:true});}
  const payload=JSON.parse(text+decoder.decode());
  if(!payload||Array.isArray(payload)||Object.keys(payload).some(key=>!['markdown','revision'].includes(key))||typeof payload.markdown!=='string')throw new Error('Передайте markdown и revision промта.');
  if(payload.markdown.includes('\0')||Array.from(payload.markdown).length>maxLength)throw new Error('Промт должен содержать не более '+maxLength.toLocaleString('ru-RU')+' символов без нулевого символа.');
  if(!Number.isSafeInteger(payload.revision)||payload.revision<0)throw new Error('Сначала загрузите текущую версию промта.');
  return payload;
 }finally{reader.releaseLock();}
}

export async function saveSystemPrompt(db,kind,input){
 if(['vacancy_extraction','vacancy_role'].includes(kind))return {...detailPrompt(await saveDetailSettings(db,'prompt',input,kind),kind),maxLength:PROMPT_MAX_LENGTHS[kind]};
 await db.prepare(SYSTEM_PROMPTS_TABLE_SQL).run();
 const row=await db.prepare(`INSERT INTO system_prompts (kind,markdown,revision,updated_at)
   SELECT ?, ?, 1, CURRENT_TIMESTAMP WHERE ? = 0 OR EXISTS(SELECT 1 FROM system_prompts WHERE kind = ? AND revision = ?)
   ON CONFLICT(kind) DO UPDATE SET markdown=excluded.markdown,revision=system_prompts.revision+1,updated_at=CURRENT_TIMESTAMP
   WHERE system_prompts.revision = ?
   RETURNING kind,markdown,revision,updated_at AS updatedAt`).bind(kind,input.markdown,input.revision,kind,input.revision,input.revision).first();
 if(!row)throw Object.assign(new Error('Промт изменён в другой вкладке. Скопируйте текст и обновите страницу перед повторным сохранением.'),{status:409});
 return {...row,maxLength:PROMPT_MAX_LENGTHS[kind]};
}
