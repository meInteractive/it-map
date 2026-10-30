import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { useDirty, confirmNavigation } = await moduleFor('frontend/hooks/useDirty.ts');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldWindow = globalThis.window, oldConfirm = globalThis.confirm;
const listeners = new Set();
let adds = 0, removes = 0, questions = 0, approved = false, root;
globalThis.window = {
  addEventListener(type, fn) {
    assert.equal(type, 'beforeunload');
    listeners.add(fn); adds++;
  },
  removeEventListener(type, fn) {
    assert.equal(type, 'beforeunload');
    assert.equal(listeners.delete(fn), true); removes++;
  },
};
globalThis.confirm = message => {
  assert.match(message, /несохранённые изменения/);
  questions++;
  return approved;
};
function Probe({ dirty }) {
  useDirty(dirty);
  return h('output', null, dirty ? 'Изменено' : 'Сохранено');
}
const view = (a, b) => h('main', null, [
  h(Probe, { key: 'a', dirty: a }),
  ...(b === null ? [] : [h(Probe, { key: 'b', dirty: b })]),
]);

try {
  // 11. Нет изменённых форм: переход без запроса подтверждения.
  await act(async () => { root = create(view(false, null)); });
  assert.equal(confirmNavigation(), true);
  assert.equal(questions, 0);
  assert.equal(listeners.size, 0);

  // 12 и 15. При dirty подписка появляется, отмена/согласие видны извне.
  await act(async () => root.update(view(true, null)));
  assert.equal(listeners.size, 1);
  assert.equal(confirmNavigation(), false);
  assert.equal(questions, 1);
  approved = true;
  assert.equal(confirmNavigation(), true);
  assert.equal(questions, 2);
  const unload = { prevented: 0, returnValue: null, preventDefault() { this.prevented++; } };
  for (const fn of listeners) fn(unload);
  assert.equal(unload.prevented, 1);
  assert.equal(unload.returnValue, '');

  // 13. Две формы: удаление/сохранение первой не снимает защиту второй.
  await act(async () => root.update(view(true, true)));
  assert.equal(listeners.size, 2);
  await act(async () => root.update(view(false, true)));
  assert.equal(listeners.size, 1);
  approved = false;
  assert.equal(confirmNavigation(), false);
  assert.equal(questions, 3);

  // 14–15. Сохранение и размонтирование последней формы снимает защиту.
  await act(async () => root.update(view(false, false)));
  assert.equal(listeners.size, 0);
  assert.equal(confirmNavigation(), true);
  assert.equal(questions, 3);
  await act(async () => root.update(view(true, true)));
  assert.equal(listeners.size, 2);
  await act(async () => root.unmount()); root = null;
  assert.equal(listeners.size, 0);
  assert.equal(confirmNavigation(), true);
  assert.equal(adds, removes);
  console.log('PASS: useDirty защищает все формы и очищает beforeunload');
} finally {
  if (root) await act(async () => root.unmount());
  if (oldWindow === undefined) delete globalThis.window; else globalThis.window = oldWindow;
  if (oldConfirm === undefined) delete globalThis.confirm; else globalThis.confirm = oldConfirm;
}
