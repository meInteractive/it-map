import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { create, act } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { ApplicationForm } = await moduleFor('frontend/features/applications/Applications.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
const valid = 'ac1.' + 'A'.repeat(43);
const payloads = [{}, { creationContext: 31 }, { creationContext: 'ac1.invalid' },
  { creationContext: valid }];
const writes = [];
let reads = 0, root, saved = 0;
globalThis.fetch = async (url, options = {}) => {
  if (url === '/api/applications/create-context' && !options.method) {
    assert.ok(reads < payloads.length, 'Неожиданный повторный GET');
    return Response.json(payloads[reads++]);
  }
  assert.equal(url, '/api/applications/create');
  assert.equal(options.method, 'POST');
  writes.push(JSON.parse(options.body));
  return Response.json({ createdApplicationId: 7, records: [{ id: 7, ...writes.at(-1) }] },
    { status: 201 });
};
const flush = () => act(async () => {
  await new Promise(resolve => setTimeout(resolve, 0));
});
const save = () => root.root.findByProps({ className: 'crm-save' });
const retry = () => root.root.findAllByType('button').find(b =>
  b.children.includes('Повторить подготовку'));
const form = () => root.root.findByType('form');

try {
  await act(async () => {
    root = create(h(ApplicationForm, {
      companyName: 'Синтетическая компания', onSaved: () => { saved++; },
    }));
  });
  await flush();
  for (let index = 0; index < payloads.length - 1; index++) {
    // 9. Даже HTTP 200 не делает некорректный контекст готовым.
    assert.equal(save().props.disabled, true);
    assert.ok(retry(), 'Ошибка контракта предлагает повторную подготовку');
    assert.match(JSON.stringify(root.toJSON()), /Не удалось подготовить создание отклика/);
    assert.equal(writes.length, 0, 'До валидного контекста нельзя отправлять POST');
    await act(async () => retry().props.onClick());
    await flush();
  }
  assert.equal(reads, 4);
  assert.equal(retry(), undefined);
  assert.equal(save().props.disabled, false);
  await act(async () => form().props.onSubmit({ preventDefault() {} }));
  assert.equal(writes.length, 1);
  assert.equal(writes[0].creationContext, valid);
  assert.match(writes[0].idempotencyKey, /^[0-9a-f-]{36}$/);
  assert.equal(saved, 1);
  console.log('PASS: три некорректных контекста отклонены; валидный разрешил POST');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = oldFetch;
}
