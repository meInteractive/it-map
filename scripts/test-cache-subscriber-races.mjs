import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const api = await moduleFor('frontend/api/client.ts');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
const pending = [];
const calls = [];
globalThis.fetch = url => {
  assert.equal(url, '/api/test-subscriber-races');
  calls.push(url);
  return new Promise(resolve => pending.push(resolve));
};
let root;
function Probe({ name }) {
  const result = api.useApi('/api/test-subscriber-races');
  return h('output', { 'data-probe': name, 'data-value': result.data?.version ?? '',
    'data-error': result.error?.message || '' });
}
function Pair({ second = true }) {
  return h('div', null, h(Probe, { name: 'first' }), second ? h(Probe, { name: 'second' }) : null);
}
const props = name => root.root.findByProps({ 'data-probe': name }).props;
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
const answer = async (data, status = 200) => {
  assert.ok(pending.length > 0, 'Ожидается управляемый запрос');
  await act(async () => pending.shift()(Response.json(data, { status })));
  await flush();
};
try {
  api.clearCache();
  // 11. Два подписчика разделяют первый запрос и его результат.
  await act(async () => { root = create(h(Pair)); });
  assert.equal(calls.length, 1);
  await answer({ version: 1 });
  assert.equal(props('first')['data-value'], 1);
  assert.equal(props('second')['data-value'], 1);

  // 12. Ошибка начальной загрузки и восстановление через invalidate.
  api.clearCache();
  await act(async () => root.update(h(Pair, { second: false })));
  await act(async () => root.update(h(Pair)));
  // clearCache не обязан немедленно запустить GET: обновляем ключ через явную инвалидaцию.
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  assert.equal(calls.length, 2);
  await answer({ error: 'Временный отказ чтения' }, 503);
  assert.match(props('first')['data-error'], /Временный отказ чтения/);
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  assert.equal(calls.length, 3);
  await answer({ version: 2 });
  assert.equal(props('first')['data-value'], 2);
  assert.equal(props('second')['data-value'], 2);

  // 13. Поздний ответ после invalidate не становится окончательным.
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  assert.equal(calls.length, 4);
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  await answer({ version: 999 });
  assert.equal(calls.length, 5, 'Активные подписчики получают повторный GET');
  assert.notEqual(props('first')['data-value'], 999);
  await answer({ version: 3 });
  assert.equal(props('second')['data-value'], 3);

  // 14. clearCache изолирует данные старого сеанса.
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  assert.equal(calls.length, 6);
  await act(async () => api.clearCache());
  assert.equal(props('first')['data-value'], '');
  await answer({ version: 111 });
  assert.equal(calls.length, 7);
  assert.notEqual(props('second')['data-value'], 111);
  await answer({ version: 4 });
  assert.equal(props('first')['data-value'], 4);

  // 15. Последний подписчик исчезает во время запроса.
  await act(async () => api.invalidate(key => key === '/api/test-subscriber-races'));
  assert.equal(calls.length, 8);
  await act(async () => root.unmount()); root = null;
  await answer({ version: 5 });
  assert.equal(calls.length, 8, 'После размонтирования нет повторного GET');
  api.clearCache();
  await act(async () => { root = create(h(Pair, { second: false })); });
  assert.equal(calls.length, 9);
  await answer({ version: 6 });
  assert.equal(props('first')['data-value'], 6);
  console.log('PASS: два подписчика, ошибка/retry, invalidate, clearCache и unmount');
} finally {
  if (root) await act(async () => root.unmount());
  api.clearCache();
  globalThis.fetch = oldFetch;
}
