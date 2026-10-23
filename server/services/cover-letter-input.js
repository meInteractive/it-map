// One coherent SQL snapshot supplies all model inputs and guards the eventual INSERT.
import {runtimeGenerationSql} from '../repositories/runtime-generation.js';
export const coverLetterInputSql = `json_array(${runtimeGenerationSql}, v.id, v.company_name, v.entity_type,
 v.real_company_name, v.url, v.title, v.description_md, v.created_at, v.updated_at,
 v.cover_letter_subject, v.cover_letter_md,
 json_array(profile.resume_md, profile.revision), json_array(prompt.markdown, prompt.revision),
 json_array(model.provider,model.openai_model,model.reasoning_effort,model.revision,CASE WHEN model.provider='openai' THEN ai_key.revision ELSE NULL END))`;
export const coverLetterInputFromSql = `company_vacancies v
 LEFT JOIN user_profiles profile ON profile.user_name = ?
 LEFT JOIN system_prompts prompt ON prompt.kind = 'cover_letter'
 LEFT JOIN prompt_model_settings model ON model.kind = 'cover_letter'
 LEFT JOIN openai_settings ai_key ON ai_key.id = 1`;

export async function readCoverLetterInput(db, userName, id) {
 return db.prepare(`SELECT ${coverLetterInputSql} AS snapshot, ${runtimeGenerationSql} AS generation,
  v.title, v.description_md AS descriptionMd,
  v.cover_letter_subject AS subject, v.cover_letter_md AS markdown,
  profile.resume_md AS resumeMd, prompt.markdown AS promptMd,
  model.provider AS provider,model.openai_model AS openaiModel,model.reasoning_effort AS reasoningEffort,
  letter.subject AS personalSubject, letter.markdown AS personalMarkdown,
  letter.override_shared AS personalOverrideShared, letter.revision AS personalRevision,
  CASE WHEN letter.user_name IS NOT NULL THEN json_array(letter.subject,letter.markdown,letter.override_shared,letter.revision) END AS personalSnapshot
  FROM ${coverLetterInputFromSql}
  LEFT JOIN vacancy_cover_letters letter ON letter.user_name = ? AND letter.vacancy_id = v.id
  WHERE v.id = ?`).bind(userName, userName, id).first();
}

export function existingCoverLetter(input) {
 if (input?.personalOverrideShared && input.personalMarkdown != null) return {coverLetterSubject: input.personalSubject, coverLetterMd: input.personalMarkdown, source: 'personal'};
 if (input?.markdown.trim()) return {coverLetterSubject: input.subject, coverLetterMd: input.markdown, source: 'vacancy'};
 if (input?.personalMarkdown != null) return {coverLetterSubject: input.personalSubject, coverLetterMd: input.personalMarkdown, source: 'personal'};
 return null;
}

export function changedCoverLetterInput() {
 return Object.assign(new Error('Данные для письма изменились во время генерации. Устаревший результат не сохранён. Обновите карточку и повторите генерацию.'), {status: 409});
}
