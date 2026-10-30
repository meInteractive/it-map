import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { create, act } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { TaskCard, scheduleIso } = await moduleFor('frontend/features/tasks/Tasks.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const previousFetch = globalThis.fetch;
const calls = [], outstanding = [];
globalThis.fetch = async (url, options = {}) => {
  assert.equal(url, '/api/tasks/77');
  assert.equal(options.method, 'PUT');
  calls.push(JSON.parse(options.body));
  return new Promise(resolve => outstanding.push(resolve));
};
const task = {
  id: 77, type: 'call', companyName: 'Синтетическая компания',
  dueAt: '2026-10-01T21:15:00.000Z', completed: false,
};
let root;
const buttons = () => root.root.findAllByType('button');
const button = label => buttons().find(b => b.children.join('') === label);
const inputs = () => root.root.findAllByType('input');
const change = async (input, value) => act(async () => input.props.onChange({ target: { value } }));
const form = () => root.root.findByType('form');
const submit = () => form().props.onSubmit({ preventDefault() {} });
try {
  await act(async () => { root = create(h(TaskCard, { task, nav: { openCompany() {}, openVacancy() {} } })); });

  // 21. Отмена формы не делает запрос.
  await act(async () => button('Изменить дату и время').props.onClick());
  assert.equal(form().props.className, 'task-edit-form');
  await act(async () => button('Отмена').props.onClick());
  assert.equal(root.root.findAllByType('form').length, 0);
  assert.equal(calls.length, 0);

  // 22–24. Московское время переводится в UTC, повтор во время ожидания запрещён.
  assert.equal(scheduleIso('2026-11-05', '12:30'), '2026-11-05T09:30:00.000Z');
  await act(async () => button('Изменить дату и время').props.onClick());
  await change(inputs().find(i => i.props.type === 'date'), '2026-11-05');
  await change(inputs().find(i => i.props.type === 'time'), '12:30');
  let pending;
  await act(() => { pending = submit(); });
  assert.deepEqual(calls, [{ dueAt: '2026-11-05T09:30:00.000Z' }]);
  assert.equal(form().props['aria-busy'], true);
  assert.equal(button('Сохранить').props.disabled, true);
  await act(() => { void submit(); });
  assert.equal(calls.length, 1);
  await act(async () => {
    outstanding.shift()(Response.json({ error: 'Синтетическая ошибка сохранения' }, { status: 503 }));
    await pending;
  });
  assert.equal(root.root.findAllByType('form').length, 1);
  assert.match(JSON.stringify(root.toJSON()), /Синтетическая ошибка сохранения/);
  assert.equal(form().props['aria-busy'], false);
  await act(() => { pending = submit(); });
  assert.equal(calls.length, 2);
  await act(async () => { outstanding.shift()(Response.json({ dueAt: calls[1].dueAt })); await pending; });
  assert.equal(root.root.findAllByType('form').length, 0);

  // 25. Выполнение задачи остаётся реакцией родителя на успешную мутацию.
  const checkbox = () => inputs().find(i => i.props.type === 'checkbox');
  let completing;
  await act(() => { completing = checkbox().props.onChange({ target: { checked: true } }); });
  assert.deepEqual(calls[2], { completed: true });
  assert.equal(checkbox().props.disabled, true);
  await act(async () => {
    outstanding.shift()(Response.json({ error: 'Синтетическая ошибка переключения' }, { status: 503 }));
    await completing;
  });
  assert.equal(checkbox().props.disabled, false);
  assert.match(JSON.stringify(root.toJSON()), /Синтетическая ошибка переключения/);
  assert.equal(checkbox().props.checked, false, 'До обновления родителя входной task не меняется');
  await act(() => { completing = checkbox().props.onChange({ target: { checked: true } }); });
  assert.equal(checkbox().props.disabled, true);
  assert.deepEqual(calls[3], { completed: true });
  await act(async () => { outstanding.shift()(Response.json({ completed: true })); await completing; });
  assert.equal(checkbox().props.disabled, false);
  console.log('PASS: TaskCard отмена, UTC, duplicate guard, retry и completion');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = previousFetch;
}
