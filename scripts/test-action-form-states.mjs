import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { ActionForm } = await moduleFor('frontend/components/Common.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
let root, current = deferred(), calls = 0;
const states = [];
const onSubmit = () => { calls++; return current.promise; };
const props = disabled => ({
  onSubmit, disabled, lockWhileBusy: true,
  onBusyChange: value => states.push(value),
  children: busy => h('input', { id: 'synthetic', disabled: busy }),
});
const form = () => root.root.findByType('form');
const fieldset = () => root.root.findByType('fieldset');
const submit = () => form().props.onSubmit({ preventDefault() {} });

try {
  await act(async () => { root = create(h(ActionForm, props(false))); });
  assert.equal(form().props['aria-busy'], false);
  assert.equal(fieldset().props.disabled, false);

  // 6–7 и 10. Один вызов, блокировка и освобождение после завершения.
  let pending;
  await act(async () => { pending = submit(); });
  assert.equal(calls, 1);
  assert.equal(form().props['aria-busy'], true);
  assert.equal(fieldset().props.disabled, true);
  await act(async () => { await submit(); });
  assert.equal(calls, 1, 'Повторная отправка ожидающего запроса запрещена');
  await act(async () => { current.resolve(); await pending; });
  assert.equal(form().props['aria-busy'], false);
  assert.equal(fieldset().props.disabled, false);
  assert.deepEqual(states, [true, false]);

  // 8. Ошибка видна, поля разблокированы, повторная отправка возможна.
  current = deferred();
  await act(async () => { pending = submit(); });
  assert.equal(calls, 2);
  assert.equal(fieldset().props.disabled, true);
  await act(async () => { current.reject(new Error('Синтетическая ошибка формы')); await pending; });
  assert.equal(fieldset().props.disabled, false);
  assert.equal(form().props['aria-busy'], false);
  const alert = root.root.findByProps({ role: 'alert' });
  assert.match(alert.children.join(''), /Синтетическая ошибка формы/);

  current = deferred();
  await act(async () => { pending = submit(); });
  assert.equal(calls, 3);
  assert.equal(root.root.findAllByProps({ role: 'alert' }).length, 0);
  await act(async () => { current.resolve(); await pending; });
  assert.equal(form().props['aria-busy'], false);
  assert.deepEqual(states, [true, false, true, false, true, false]);

  // 9. Внешний disabled блокирует отправку ещё до вызова onSubmit.
  await act(async () => { root.update(h(ActionForm, props(true))); });
  assert.equal(form().props['aria-busy'], true);
  assert.equal(fieldset().props.disabled, true);
  await act(async () => { await submit(); });
  assert.equal(calls, 3);
  await act(async () => { root.update(h(ActionForm, props(false))); });
  assert.equal(form().props['aria-busy'], false);
  assert.equal(fieldset().props.disabled, false);
  console.log('PASS: ActionForm, сохранение, повтор, ошибка и блокировка');
} finally {
  if (root) await act(async () => root.unmount());
}
