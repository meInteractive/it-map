const columns = ['id', 'company_name', 'entity_type', 'real_company_name', 'url',
  'title', 'role', 'level', 'salary_from', 'salary_to', 'relevance', 'published_at',
  'loaded_at', 'last_checked_at', 'archived', 'description_md', 'cover_letter_subject',
  'cover_letter_md', 'application_status', 'created_at', 'updated_at'];

// Three session-user bindings; use this exact expression in the detail SELECT
// and conditional UPDATE. Never hash the effective shared/personal projection.
export function vacancyStateSql(alias = 'v') {
  return `json_array(?, ${columns.map(column => alias + '.' + column).join(', ')},
    (SELECT json_group_array(json_array(id, channel_id, message_url, telegram_contact, created_at, updated_at))
     FROM (SELECT id, channel_id, message_url, telegram_contact, created_at, updated_at
       FROM vacancy_telegram_sources WHERE vacancy_id = ${alias}.id ORDER BY id)),
    (SELECT json_group_array(role_id) FROM (SELECT role_id FROM vacancy_role_assignments WHERE vacancy_id = ${alias}.id ORDER BY role_id)),
    (SELECT relevance FROM company_vacancy_user_state WHERE user_name = ? AND vacancy_id = ${alias}.id),
    (SELECT json_array(subject, markdown, override_shared, revision) FROM vacancy_cover_letters WHERE user_name = ? AND vacancy_id = ${alias}.id))`;
}
export const vacancyStateBindings = userName => [userName, userName, userName];
export async function vacancyRevision(state) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(state));
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}
export async function readVacancyState(db, userName, id) {
  return db.prepare(`SELECT ${vacancyStateSql()} AS snapshot FROM company_vacancies v WHERE v.id = ?`)
    .bind(...vacancyStateBindings(userName), id).first();
}
export function vacancyConflict() {
  return Object.assign(new Error('Вакансия изменена в другой вкладке. Черновик остаётся в форме; проверьте актуальную версию перед повторным редактированием.'), {status: 409});
}
