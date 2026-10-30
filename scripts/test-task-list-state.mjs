import assert from 'node:assert/strict';
import fs from 'node:fs';
import { createElement as h } from 'react';
import { create, act } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

fs.mkdirSync('.tests', { recursive: true });
fs.writeFileSync('.tests/task-list-entry.tsx',
  "export {default as Tasks} from '../frontend/features/tasks/Tasks'; export {clearCache} from '../frontend/api/client';");
const { Tasks, clearCache } = await moduleFor('.tests/task-list-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
let root, failId = null;
const writes = [], reads = [];
const entities = [
  { name: 'Компания А', entityType: 'company' },
  { name: 'Компания Б', entityType: 'company' },
  { name: 'Агентство В', entityType: 'agency' },
];
let tasks = [
  { id: 11, type: 'call', companyName: 'Компания А', completed: false,
    dueAt: '2026-11-05T12:00:00.000Z', note: 'Задача А' },
  { id: 12, type: 'email', companyName: 'Компания Б', completed: false,
    dueAt: '2026-11-05T13:00:00.000Z', note: 'Задача Б' },
  { id: 13, type: 'meeting', companyName: 'Агентство В', completed: true,
    dueAt: '2026-11-05T14:00:00.000Z', note: 'Задача В' },
];
globalThis.fetch = async (url, options = {}) => {
  if (url === '/api/google-calendar/status') return Response.json({ configured: false });
  if (url.startsWith('/api/tasks/') && options.method === 'PUT') {
    const id = Number(url.slice('/api/tasks/'.length));
    const body = JSON.parse(options.body);
    writes.push({ id, body });
    if (id === failId) return Response.json({ error: 'Ошибка задачи ' + id }, { status: 503 });
    tasks = tasks.map(t => t.id === id ? { ...t, ...body } : t);
    return Response.json({ task: tasks.find(t => t.id === id) });
  }
  assert.ok(url === '/api/tasks' || url === '/api/tasks?includeCompleted=1',
    'Неожиданный API: ' + url);
  reads.push(url);
  return Response.json({ entities, tasks: url.includes('includeCompleted=1')
    ? tasks : tasks.filter(t => !t.completed) });
};
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
const cards = () => root.root.findAll(n => n.type === 'article' &&
  typeof n.props.className === 'string' && n.props.className.startsWith('task-card'));
const card = name => cards().find(n => n.findAllByProps({ className: 'task-note' })
  .some(x => x.children.includes(name)));
const names = () => cards().map(n =>
  n.findByProps({ className: 'task-note' }).children.join(''));
const completed = name => card(name).findByProps({ className: 'task-complete' }).findByType('input');
const count = () => Number(root.root.findByProps({ className: 'applications-count' }).children.join(''));

try {
  clearCache();
  await act(async () => { root = create(h(Tasks, { nav: { openCompany() {}, openVacancy() {} } })); });
  await flush();
  // 21. Исходный список содержит две незавершённые задачи компаний.
  assert.deepEqual(names(), ['Задача А', 'Задача Б']);
  assert.equal(count(), 2);
  await act(async () => completed('Задача А').props.onChange({ target: { checked: true } }));
  await flush();
  assert.deepEqual(writes[0], { id: 11, body: { completed: true } });

  // 22. Успешная мутация инвалидирует список: обновлённый GET без дубликатов.
  assert.ok(reads.filter(x => x === '/api/tasks').length >= 2);
  assert.deepEqual(names(), ['Задача Б']);
  assert.equal(count(), 1);
  assert.equal(new Set(cards().map(n => n.findByProps({ className: 'task-note' }).children.join(''))).size,
    cards().length);

  // 23. Показывать завершённые: другой ключ и две карточки организаций.
  const showCompleted = root.root.findByProps({ className: 'show-completed' }).findByType('input');
  await act(async () => showCompleted.props.onChange({ target: { checked: true } }));
  await flush();
  assert.ok(reads.includes('/api/tasks?includeCompleted=1'));
  assert.deepEqual(names(), ['Задача А', 'Задача Б']);
  assert.equal(completed('Задача А').props.checked, true);
  assert.equal(count(), 2);

  // 24. Ошибка привязана только к нужной карточке, соседняя неизменна.
  failId = 12;
  await act(async () => completed('Задача Б').props.onChange({ target: { checked: true } }));
  await flush();
  assert.deepEqual(writes.at(-1), { id: 12, body: { completed: true } });
  assert.match(JSON.stringify(card('Задача Б').toJSON()), /Ошибка задачи 12/);
  assert.doesNotMatch(JSON.stringify(card('Задача А').toJSON()), /Ошибка задачи 12/);
  assert.equal(completed('Задача Б').props.checked, false);
  assert.equal(completed('Задача А').props.checked, true);

  // 25. Смена типа переключает реальные карточки и счётчик, без запроса на запись.
  const writesBefore = writes.length;
  const agency = root.root.findAllByType('button').find(b => b.children.join('') === 'Агентства');
  await act(async () => agency.props.onClick());
  assert.deepEqual(names(), ['Задача В']);
  assert.equal(count(), 1);
  assert.equal(writes.length, writesBefore);
  console.log('PASS: задачный список, инвалидация, completed, ошибка и агентства');
} finally {
  if (root) await act(async () => root.unmount());
  clearCache();
  globalThis.fetch = oldFetch;
}
