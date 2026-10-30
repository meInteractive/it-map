import assert from 'node:assert/strict';
import fs from 'node:fs';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

fs.mkdirSync('.tests', { recursive: true });
fs.writeFileSync('.tests/role-tree-read-states.tsx',
  "export {RoleTreeOptions,VacancyRolePicker} from '../frontend/features/vacancies/RoleTree'; export {clearCache} from '../frontend/api/client';");
const ui = await moduleFor('.tests/role-tree-read-states.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const previousFetch = globalThis.fetch;
let root;
const label = () => JSON.stringify(root.toJSON());
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });

try {
  // 19. Показываем загрузку до получения каталога, без ложного пустого результата.
  let resolve;
  globalThis.fetch = url => {
    assert.equal(url, '/api/vacancy-roles');
    return new Promise(next => { resolve = next; });
  };
  await act(async () => {
    root = create(h(ui.RoleTreeOptions, { selected: [], onChange() {} }));
  });
  assert.match(label(), /Загрузка ролей…/);

  // 20. Серверная ошибка синтетическая; UI должен показать своё сообщение.
  await act(async () => resolve(Response.json({ error: 'Тестовая ошибка' }, { status: 503 })));
  await flush();
  assert.match(label(), /Не удалось загрузить роли/);
  assert.doesNotMatch(label(), /Загрузка ролей…/);
  await act(async () => root.unmount()); root = undefined;
  ui.clearCache();

  // 21. Пустой выбор конечных ролей после корректного пустого каталога.
  globalThis.fetch = async url => {
    assert.equal(url, '/api/vacancy-roles');
    return Response.json({ roles: [], roots: [], leaves: [], tree: [] });
  };
  await act(async () => {
    root = create(h(ui.VacancyRolePicker, { selected: [], onChange() {} }));
  });
  await flush();
  const selected = root.root.findByProps({ className: 'vacancy-role-selection' });
  assert.deepEqual(selected.children, ['Роли не выбраны']);
  assert.equal(root.root.findAllByType('input').length, 0);
  console.log('PASS: RoleTree — загрузка, ошибка и пустой выбор');
} finally {
  if (root) await act(async () => root.unmount());
  ui.clearCache();
  globalThis.fetch = previousFetch;
}
