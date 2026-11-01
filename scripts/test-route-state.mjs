import assert from 'node:assert/strict';
import {moduleFor} from './test-support.mjs';

// Чистая проверка маршрутов без сети, куки и приватных данных.
const {sectionFromPath, sectionPath, rememberLoginDestination, restoreLoginDestination} =
  await moduleFor('frontend/lib/routes.ts');
for (const section of ['tasks', 'applications', 'vacancies', 'list', 'map', 'settings']) {
  const path = sectionPath(section);
  assert.equal(sectionFromPath(path), section);
  assert.equal(sectionFromPath(path + (path.endsWith('/') ? '' : '/')), section);
}
for (const invalid of ['/unknown', '/vacancies/extra', '/list/7', '/logout', '/auth', '//list']) {
  assert.equal(sectionFromPath(invalid), 'tasks');
}
const map = new Map();
const storage = {
  getItem: key => map.get(key) || null,
  setItem: (key, value) => { map.set(key, value); },
  removeItem: key => { map.delete(key); },
};
rememberLoginDestination('/vacancies', storage);
assert.equal(restoreLoginDestination('/', storage), 'vacancies');
assert.equal(restoreLoginDestination('/', storage), 'tasks', 'Редирект используется только один раз');
rememberLoginDestination('/map', storage);
assert.equal(restoreLoginDestination('/list', storage), 'list', 'Прямой путь имеет приоритет');
assert.equal(restoreLoginDestination('/', storage), 'map');
storage.setItem('itmap:post-login-section', 'admin-secrets');
assert.equal(restoreLoginDestination('/', storage), 'tasks', 'Непроверенный раздел не открывается');
console.log('PASS: URL разделов, fallback, одноразовый возврат после входа');
