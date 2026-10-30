import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { vacancyRoleText } = await moduleFor('frontend/features/vacancies/RoleTree.tsx');
const first = { id: 2, name: 'Разработчик', path: 'ИТ → Разработчик' };
const second = { id: 4, name: 'Редактор', path: 'Медиа → Редактор' };
assert.equal(vacancyRoleText({ roles: [first] }), 'ИТ → Разработчик');
assert.equal(vacancyRoleText({ roles: [first, second] }),
  'ИТ → Разработчик; Медиа → Редактор');
assert.equal(vacancyRoleText({ roles: [second, first] }),
  'Медиа → Редактор; ИТ → Разработчик', 'Порядок задаёт входной массив');
assert.equal(vacancyRoleText({ roles: [first, second] }, false), 'Разработчик; Редактор');
assert.equal(vacancyRoleText({ roles: [], role: 'Старое текстовое название' }),
  'Старое текстовое название');
assert.equal(vacancyRoleText({ role: 'Старое текстовое название' }),
  'Старое текстовое название');
assert.equal(vacancyRoleText({ roles: [] }), '');
console.log('PASS: пути ролей, разделитель и legacy fallback');
