import {ensureCrmSchema} from './schema.js';

const MAX_ROLES_PER_VACANCY = 12;

function roleName(value) {
  const name = String(value || '').trim().replace(/\s+/g, ' ');
  if (!name) throw new Error('Укажите название роли.');
  if (name.length > 120) throw new Error('Название роли не должно превышать 120 символов.');
  return name;
}

function roleId(value, label = 'Роль') {
  const id = Number(value);
  if (!Number.isSafeInteger(id) || id < 1) throw new Error(`${label} указана некорректно.`);
  return id;
}

function buildRoleCatalog(rows) {
  const roles = rows.map(row => ({
    id: Number(row.id),
    stableKey: String(row.stableKey),
    name: String(row.name),
    parentId: row.parentId == null ? null : Number(row.parentId),
    sortOrder: Number(row.sortOrder || 0),
    usageCount: Number(row.usageCount || 0),
    childCount: Number(row.childCount || 0)
  }));
  const byId = new Map(roles.map(role => [role.id, role]));
  const pathFor = role => {
    const names = [];
    const seen = new Set();
    let current = role;
    while (current) {
      if (seen.has(current.id)) throw new Error('В дереве ролей обнаружен цикл.');
      seen.add(current.id);
      names.unshift(current.name);
      current = current.parentId == null ? null : byId.get(current.parentId);
    }
    return names.join(' → ');
  };
  roles.forEach(role => {
    role.isLeaf = role.childCount === 0;
    role.path = pathFor(role);
  });
  const children = new Map();
  roles.forEach(role => {
    const key = role.parentId ?? 0;
    children.set(key, [...(children.get(key) || []), role]);
  });
  children.forEach(items => items.sort((a, b) => a.sortOrder - b.sortOrder || a.name.localeCompare(b.name, 'ru') || a.id - b.id));
  const descendants = role => (children.get(role.id) || []).flatMap(child => [child.id, ...descendants(child)]);
  roles.forEach(role => { role.descendantIds = descendants(role); });
  const nest = parentId => (children.get(parentId ?? 0) || []).map(role => ({...role, children: nest(role.id)}));
  const byName = new Map(roles.map(role => [role.name.toLocaleLowerCase('ru'), role]));
  const byPath = new Map(roles.map(role => [role.path.toLocaleLowerCase('ru'), role]));
  return {roles, tree: nest(null), roots: roles.filter(role => role.parentId == null), leaves: roles.filter(role => role.isLeaf), byId, byName, byPath};
}

export async function readVacancyRoleCatalog(db) {
  const result = await db.prepare(`SELECT role.id,role.stable_key AS stableKey,role.name,role.parent_id AS parentId,
      role.sort_order AS sortOrder,COUNT(DISTINCT assignment.vacancy_id) AS usageCount,
      COUNT(DISTINCT child.id) AS childCount
    FROM vacancy_roles role
    LEFT JOIN vacancy_role_assignments assignment ON assignment.role_id=role.id
    LEFT JOIN vacancy_roles child ON child.parent_id=role.id
    GROUP BY role.id,role.stable_key,role.name,role.parent_id,role.sort_order
    ORDER BY role.parent_id IS NOT NULL,role.parent_id,role.sort_order,role.id`).all();
  return buildRoleCatalog(result?.results || []);
}

export async function readVacancyRoles(db) {
  const {roles,tree,roots,leaves} = await readVacancyRoleCatalog(db);
  return {roles,tree,roots,leaves};
}

export function vacancyRoleSelection(catalog, payload, {allowEmpty = false} = {}) {
  let selected;
  if (Array.isArray(payload?.roleIds)) {
    selected = payload.roleIds.map(value => roleId(value));
  } else {
    const legacy = String(payload?.role || '').trim();
    const values = legacy ? legacy.split(/\s*;\s*/).filter(Boolean) : [];
    selected = values.map(value => {
      const key = value.toLocaleLowerCase('ru');
      const match = catalog.byPath.get(key) || catalog.byName.get(key)
        || (key === 'менеджер продукта' ? catalog.byName.get('менеджер продуктов') : null);
      if (!match) throw new Error(`Роль «${value}» не найдена в справочнике.`);
      return match.id;
    });
  }
  selected = [...new Set(selected)];
  if (selected.length > MAX_ROLES_PER_VACANCY) throw new Error(`Для вакансии можно выбрать не более ${MAX_ROLES_PER_VACANCY} ролей.`);
  if (!selected.length && !allowEmpty) throw new Error('Укажите хотя бы одну роль вакансии.');
  for (const id of selected) {
    const role = catalog.byId.get(id);
    if (!role) throw new Error('Одна из выбранных ролей больше не существует.');
    if (!role.isLeaf) throw new Error(`«${role.path}» является категорией. Выберите конечную роль.`);
  }
  const chosen = selected.map(id => catalog.byId.get(id));
  return {roleIds: selected, role: chosen.map(item => item.name).join('; '), rolePaths: chosen.map(item => item.path)};
}

export function vacancyRoleAssignmentStatements(db, vacancyIdExpression, vacancyIdBinding, roleIds) {
  const idSql = vacancyIdExpression || '?';
  const binding = vacancyIdBinding == null ? [] : [vacancyIdBinding];
  return [
    db.prepare(`DELETE FROM vacancy_role_assignments WHERE vacancy_id=${idSql}`).bind(...binding),
    db.prepare(`INSERT INTO vacancy_role_assignments(vacancy_id,role_id)
      SELECT ${idSql},CAST(value AS INTEGER) FROM json_each(?)`).bind(...binding, JSON.stringify(roleIds))
  ];
}

export async function attachVacancyRoles(db, vacancies, suppliedCatalog = null) {
  if (!vacancies.length) return vacancies;
  const ids = vacancies.map(item => Number(item.id));
  const [catalog, result] = await Promise.all([
    suppliedCatalog || readVacancyRoleCatalog(db),
    db.prepare(`SELECT vacancy_id AS vacancyId,role_id AS roleId FROM vacancy_role_assignments
      WHERE vacancy_id IN (SELECT CAST(value AS INTEGER) FROM json_each(?)) ORDER BY vacancy_id,role_id`).bind(JSON.stringify(ids)).all()
  ]);
  const assigned = new Map();
  for (const row of result?.results || []) {
    const role = catalog.byId.get(Number(row.roleId));
    if (role) assigned.set(Number(row.vacancyId), [...(assigned.get(Number(row.vacancyId)) || []), role]);
  }
  return vacancies.map(vacancy => {
    const roles = (assigned.get(Number(vacancy.id)) || []).sort((a, b) => a.path.localeCompare(b.path, 'ru'));
    return {...vacancy, roles, role: roles.length ? roles.map(item => item.name).join(', ') : vacancy.role || ''};
  });
}

async function assertUniqueName(db, name, exceptId = 0) {
  const rows = await db.prepare('SELECT id,name FROM vacancy_roles WHERE id<>?').bind(exceptId).all();
  const normalized = name.toLocaleLowerCase('ru');
  const duplicate = (rows?.results || []).find(role => String(role.name).toLocaleLowerCase('ru') === normalized);
  if (duplicate) throw new Error('Роль с таким названием уже существует.');
}

async function nextSortOrder(db, parentId) {
  const row = parentId == null
    ? await db.prepare('SELECT COALESCE(MAX(sort_order),0)+10 AS value FROM vacancy_roles WHERE parent_id IS NULL').first()
    : await db.prepare('SELECT COALESCE(MAX(sort_order),0)+10 AS value FROM vacancy_roles WHERE parent_id=?').bind(parentId).first();
  return Number(row?.value || 10);
}

export async function createVacancyRole(db, payload) {
  await ensureCrmSchema(db);
  const name = roleName(payload?.name);
  const parentId = payload?.parentId == null || payload.parentId === '' ? null : roleId(payload.parentId, 'Родительская роль');
  if (parentId != null) {
    const parent = await db.prepare(`SELECT id,
      (SELECT COUNT(*) FROM vacancy_role_assignments assignment WHERE assignment.role_id=vacancy_roles.id) AS usageCount
      FROM vacancy_roles WHERE id=?`).bind(parentId).first();
    if (!parent) throw new Error('Родительская роль не найдена.');
    if (Number(parent.usageCount)) throw new Error('Используемая роль должна оставаться листом. Сначала перенесите вакансии на другую роль.');
  }
  await assertUniqueName(db, name);
  const sortOrder = await nextSortOrder(db, parentId);
  const stableKey = `role-${crypto.randomUUID()}`;
  await db.prepare(`INSERT INTO vacancy_roles(stable_key,name,parent_id,sort_order,created_at,updated_at)
    VALUES(?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`).bind(stableKey, name, parentId, sortOrder).run();
  return readVacancyRoles(db);
}

export async function updateVacancyRole(db, idValue, payload) {
  await ensureCrmSchema(db);
  const id = roleId(idValue);
  const current = await db.prepare('SELECT id,name,parent_id AS parentId,sort_order AS sortOrder FROM vacancy_roles WHERE id=?').bind(id).first();
  if (!current) return null;
  const name = Object.hasOwn(payload || {}, 'name') ? roleName(payload.name) : current.name;
  const parentId = Object.hasOwn(payload || {}, 'parentId')
    ? (payload.parentId == null || payload.parentId === '' ? null : roleId(payload.parentId, 'Родительская роль'))
    : (current.parentId == null ? null : Number(current.parentId));
  let sortOrder = Object.hasOwn(payload || {}, 'sortOrder') ? Number(payload.sortOrder) : Number(current.sortOrder || 0);
  if (!Number.isSafeInteger(sortOrder) || sortOrder < 0) throw new Error('Порядок роли указан некорректно.');
  if (parentId === id) throw new Error('Роль нельзя сделать родителем самой себя.');
  if (parentId != null) {
    const parent = await db.prepare(`SELECT id,
      (SELECT COUNT(*) FROM vacancy_role_assignments assignment WHERE assignment.role_id=vacancy_roles.id) AS usageCount
      FROM vacancy_roles WHERE id=?`).bind(parentId).first();
    if (!parent) throw new Error('Родительская роль не найдена.');
    if (Number(parent.usageCount)) throw new Error('Используемая роль должна оставаться листом. Сначала перенесите вакансии на другую роль.');
    const cycle = await db.prepare(`WITH RECURSIVE descendants(id) AS (
      SELECT id FROM vacancy_roles WHERE parent_id=? UNION ALL
      SELECT role.id FROM vacancy_roles role JOIN descendants d ON role.parent_id=d.id
    ) SELECT id FROM descendants WHERE id=? LIMIT 1`).bind(id, parentId).first();
    if (cycle) throw new Error('Перемещение создаёт цикл в дереве ролей.');
  }
  await assertUniqueName(db, name, id);
  if (parentId !== (current.parentId == null ? null : Number(current.parentId)) && !Object.hasOwn(payload || {}, 'sortOrder')) sortOrder = await nextSortOrder(db, parentId);
  await db.prepare(`UPDATE vacancy_roles SET name=?,parent_id=?,sort_order=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`)
    .bind(name, parentId, sortOrder, id).run();
  return readVacancyRoles(db);
}

export async function deleteVacancyRole(db, idValue) {
  await ensureCrmSchema(db);
  const id = roleId(idValue);
  const role = await db.prepare(`SELECT id,name,
    (SELECT COUNT(*) FROM vacancy_roles child WHERE child.parent_id=vacancy_roles.id) AS childCount,
    (SELECT COUNT(*) FROM vacancy_role_assignments a WHERE a.role_id=vacancy_roles.id) AS usageCount
    FROM vacancy_roles WHERE id=?`).bind(id).first();
  if (!role) return null;
  if (Number(role.usageCount)) throw new Error(`Роль «${role.name}» используется в вакансиях и не может быть удалена.`);
  if (Number(role.childCount)) throw new Error('Сначала переместите или удалите дочерние роли.');
  if(await db.prepare('SELECT id FROM vacancy_classifications WHERE root_role_id=? LIMIT 1').bind(id).first())throw new Error('Роль имеет историю классификаций и не может быть удалена.');
  const configured=await db.prepare('SELECT role_id FROM vacancy_role_classification WHERE role_id=? AND (parsing_enabled=1 OR length(trim(markdown))>0)').bind(id).first();
  if(configured)throw new Error('Сначала выключите парсинг и очистите промт классификации этой роли.');
  const removed=await db.prepare('DELETE FROM vacancy_roles WHERE id=? AND NOT EXISTS(SELECT 1 FROM vacancy_role_classification WHERE role_id=? AND (parsing_enabled=1 OR length(trim(markdown))>0))').bind(id,id).run();
  if(!removed.meta?.changes)throw new Error('Настройки роли изменились. Обновите страницу.');
  return readVacancyRoles(db);
}
