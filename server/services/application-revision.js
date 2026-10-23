// Fingerprint persisted state, not a seconds-resolution timestamp or display projection.
// The same SQL snapshot is also the atomic UPDATE predicate. No new stored revision
// or backup field is needed; all writers (including transfers/imports) change the token.
const columns = ['id', 'user_name', 'company_name', 'vacancy_id', 'status', 'comment_md',
  'recruiter_name', 'recruiter_phone', 'recruiter_email', 'recruiter_contact_type',
  'application_sources', 'created_at', 'updated_at'];

export function applicationStateSql(alias = '') {
  return `json_array(${columns.map(column => alias + column).join(', ')})`;
}

export async function applicationRevision(state) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(state));
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

export function applicationConflict() {
  return Object.assign(new Error('Отклик изменён в другой вкладке. Ваши правки сохранены в форме; загрузите актуальные данные перед повторным сохранением.'), {status: 409});
}
