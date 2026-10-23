const keys = new Map([
  ['менеджер проектов', 'it-project-manager'], ['менеджер продукта', 'it-product-manager'],
  ['менеджер продуктов', 'it-product-manager'], ['руководитель разработки', 'it-development-manager'],
  ['директор по разработке', 'it-development-director'], ['редактор', 'journalist-editor'],
  ['дата-журналист', 'journalist-data'], ['it manager', 'it-unspecified'], ['журналист', 'journalist-unspecified']
]);
const normalize = value => String(value || '').trim().replace(/\s+/g, ' ').toLocaleLowerCase('ru');

// Resolve the entire value or retain it intact. Never turn an unknown component
// into a guessed role, or treat a standard multi-selection as one new leaf.
export function legacyVacancyRoleIds(value, roles, {standardOnly = false} = {}) {
  const byId = new Map(roles.map(role => [Number(role.id), role]));
  const byKey = new Map(roles.map(role => [role.stable_key, role]));
  const leaves = roles.filter(role => !roles.some(child => Number(child.parent_id) === Number(role.id)));
  const ids = String(value || '').split(/\s*[;,]\s*/).filter(Boolean).map(part => {
    const token = normalize(part.split('→').at(-1));
    const standard = byKey.get(keys.get(token));
    const expectedRoot = String(standard?.stable_key || '').startsWith('journalist-') ? 'root-journalist' : 'root-it-manager';
    if (standard && leaves.includes(standard) && byId.get(Number(standard.parent_id))?.stable_key === expectedRoot) return Number(standard.id);
    if (!standardOnly) {
      const matches = leaves.filter(role => normalize(role.name) === normalize(part));
      if (matches.length === 1) return Number(matches[0].id);
    }
    return null;
  });
  if (ids.length && ids.every(id => id !== null)) return [...new Set(ids)];
  if (!standardOnly) {
    const matches = leaves.filter(role => normalize(role.name) === normalize(value));
    if (matches.length === 1) return [Number(matches[0].id)];
  }
  return null;
}

export function preservedRoleReplacements(roles) {
  const root = roles.find(role => role.stable_key === 'root-preserved' && role.parent_id == null);
  if (!root) return [];
  return roles.filter(role => Number(role.parent_id) === Number(root.id) && String(role.stable_key).startsWith('legacy-'))
    .flatMap(role => {
      const ids = legacyVacancyRoleIds(role.name, roles, {standardOnly:true});
      return ids ? [{role, ids}] : [];
    });
}

export async function repairLegacyVacancyRoles(db) {
  const roles = (await db.prepare('SELECT id,stable_key,name,parent_id FROM vacancy_roles').all()).results || [];
  const tables = new Set(((await db.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name IN ('vacancy_parser_roles','vacancy_classifications','vacancy_role_classification')").all()).results || []).map(row => row.name));
  const statements = [];
  for (const {role, ids} of preservedRoleReplacements(roles)) {
    // Every destructive statement checks the observed legacy identity again.
    const guard = 'EXISTS(SELECT 1 FROM vacancy_roles WHERE id=? AND stable_key=? AND name=? AND parent_id=?)';
    const identity = [role.id, role.stable_key, role.name, role.parent_id];
    for (const id of ids) {
      statements.push(db.prepare(`INSERT OR IGNORE INTO vacancy_role_assignments(vacancy_id,role_id) SELECT vacancy_id,? FROM vacancy_role_assignments WHERE role_id=? AND ${guard}`).bind(id, role.id, ...identity));
      if (tables.has('vacancy_parser_roles')) statements.push(db.prepare(`INSERT OR IGNORE INTO vacancy_parser_roles(vacancy_id,role_id,candidate_id) SELECT vacancy_id,?,candidate_id FROM vacancy_parser_roles WHERE role_id=? AND ${guard}`).bind(id, role.id, ...identity));
    }
    statements.push(db.prepare(`UPDATE company_vacancies SET role=(SELECT group_concat(name,'; ') FROM (SELECT r.name FROM vacancy_role_assignments a JOIN vacancy_roles r ON r.id=a.role_id WHERE a.vacancy_id=company_vacancies.id AND r.id<>? ORDER BY r.sort_order,r.id)) WHERE id IN (SELECT vacancy_id FROM vacancy_role_assignments WHERE role_id=?) AND ${guard}`).bind(role.id, role.id, ...identity));
    statements.push(db.prepare(`DELETE FROM vacancy_role_assignments WHERE role_id=? AND ${guard}`).bind(role.id, ...identity));
    statements.push(db.prepare(`DELETE FROM vacancy_roles WHERE id=? AND ${guard}
      AND NOT EXISTS(SELECT 1 FROM vacancy_roles child WHERE child.parent_id=vacancy_roles.id)
      ${tables.has('vacancy_classifications') ? 'AND NOT EXISTS(SELECT 1 FROM vacancy_classifications WHERE root_role_id=vacancy_roles.id)' : ''}
      ${tables.has('vacancy_role_classification') ? "AND NOT EXISTS(SELECT 1 FROM vacancy_role_classification WHERE role_id=vacancy_roles.id AND (parsing_enabled=1 OR length(trim(markdown))>0))" : ''}`).bind(role.id, ...identity));
  }
  const unassigned = (await db.prepare("SELECT id,role FROM company_vacancies v WHERE trim(role)<>'' AND NOT EXISTS(SELECT 1 FROM vacancy_role_assignments WHERE vacancy_id=v.id)").all()).results || [];
  for (const vacancy of unassigned) {
    const ids = legacyVacancyRoleIds(vacancy.role, roles);
    if (ids) statements.push(db.prepare(`INSERT OR IGNORE INTO vacancy_role_assignments(vacancy_id,role_id) SELECT ?,CAST(value AS INTEGER) FROM json_each(?) WHERE EXISTS(SELECT 1 FROM company_vacancies WHERE id=? AND role=?) AND NOT EXISTS(SELECT 1 FROM vacancy_role_assignments WHERE vacancy_id=?)`).bind(vacancy.id, JSON.stringify(ids), vacancy.id, vacancy.role, vacancy.id));
  }
  statements.push(db.prepare(`DELETE FROM vacancy_roles WHERE stable_key='root-preserved' AND parent_id IS NULL
    AND NOT EXISTS(SELECT 1 FROM vacancy_roles child WHERE child.parent_id=vacancy_roles.id)
    AND NOT EXISTS(SELECT 1 FROM vacancy_role_assignments WHERE role_id=vacancy_roles.id)
    ${tables.has('vacancy_classifications') ? 'AND NOT EXISTS(SELECT 1 FROM vacancy_classifications WHERE root_role_id=vacancy_roles.id)' : ''}
    ${tables.has('vacancy_role_classification') ? "AND NOT EXISTS(SELECT 1 FROM vacancy_role_classification WHERE role_id=vacancy_roles.id AND (parsing_enabled=1 OR length(trim(markdown))>0))" : ''}`));
  await db.batch(statements);
}
