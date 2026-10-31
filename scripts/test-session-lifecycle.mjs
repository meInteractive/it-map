import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act, create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

// Реальный API-клиент и ActionForm, только управляемые синтетические HTTP-ответы.
const api = await moduleFor('frontend/api/client.ts');
const {ActionForm} = await moduleFor('frontend/components/Common.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch, oldWindow = globalThis.window;
let root, expirations = 0;
const calls = [];
globalThis.window = {
  dispatchEvent(event) {
    if (event.type === 'session-expired') expirations++;
    return true;
  },
};
globalThis.fetch = (url, options = {}) => new Promise(resolve => {
  calls.push({url, method: options.method || 'GET', resolve});
});
const flush = () => act(async () => { await new Promise(resolve => setTimeout(resolve, 0)); });
const answer = async (call, body, status = 200) => {
  assert.ok(call && typeof call.resolve === 'function', 'Запрос должен реально начаться');
  await act(async () => { call.resolve(Response.json(body, {status})); await new Promise(resolve => setTimeout(resolve, 0)); });
};
const next = (url, method = 'GET', offset = 0) =>
  calls.filter(call => call.url === url && call.method === method).at(offset);
const count = (url, method = 'GET') => calls.filter(call => call.url === url && call.method === method).length;
function Probe({path, name}) {
  const {data, error} = api.useApi(path);
  return h('output', {'data-probe': name, 'data-value': data?.privateValue || '',
    'data-error': error?.message || ''});
}
const value = name => root.root.findByProps({'data-probe': name}).props['data-value'];
const stop = async () => {
  if (root) await act(async () => root.unmount());
  root = null;
  api.clearCache();
};
try {
  // 01, 02: приватные данные списка и открытой вакансии очищаются вместе при 401.
  await act(async () => {
    root = create(h('main', null,
      h(Probe, {path: '/api/companies/list', name: 'companies'}),
      h(Probe, {path: '/api/vacancies/71', name: 'vacancy'})));
  });
  await answer(next('/api/companies/list'), {privateValue: 'Данные аккаунта А'});
  await answer(next('/api/vacancies/71'), {privateValue: 'Личное письмо аккаунта А'});
  assert.equal(value('companies'), 'Данные аккаунта А');
  assert.equal(value('vacancy'), 'Личное письмо аккаунта А');
  await act(async () => api.invalidate(key => key === '/api/vacancies/71'));
  await answer(next('/api/vacancies/71', 'GET', 1), {error: 'Сессия истекла'}, 401);
  assert.equal(value('companies'), '');
  assert.equal(value('vacancy'), '');
  assert.equal(expirations, 1);
  await stop();

  // 03, 04: успешная проверка сессии разрешает новое чтение, но не старый кэш.
  const checkSession = api.request('/api/session');
  await answer(next('/api/session'), {name: 'Аккаунт Б', isAdmin: false});
  assert.deepEqual(await checkSession, {name: 'Аккаунт Б', isAdmin: false});
  await act(async () => { root = create(h(Probe, {path: '/api/companies/list', name: 'companies'})); });
  assert.equal(value('companies'), '', 'До нового ответа нет приватных строк аккаунта А');
  await answer(next('/api/companies/list', 'GET', 1), {privateValue: 'Данные аккаунта Б'});
  assert.equal(value('companies'), 'Данные аккаунта Б');
  await stop();

  // 05: ответ старого GET после logout/reset не попадает в кэш.
  const oldRead = api.request('/api/companies/list');
  const pendingRead = next('/api/companies/list', 'GET', 2);
  api.clearCache();
  await answer(pendingRead, {privateValue: 'Просроченное чтение'});
  await assert.rejects(oldRead, error => error instanceof api.ApiError && error.status === 409);

  // 06: несколько одновременных 401 не создают каскадный выход.
  const countBefore = expirations;
  const first = api.request('/api/vacancies/71');
  const second = api.request('/api/channels');
  await answer(next('/api/vacancies/71', 'GET', 2), {error: 'Сессия истекла'}, 401);
  await assert.rejects(first, error => error.status === 401);
  await answer(next('/api/channels'), {error: 'Сессия истекла'}, 401);
  await assert.rejects(second, error => error.status === 409);
  assert.equal(expirations, countBefore + 1);

  // 07: 403 при записи не удаляет несохранённые поля и не сообщает об успехе.
  let saves = 0, formCalls = 0, submitPromise;
  const Editor = () => {
    const [title, setTitle] = (awaitReact.useState)('Синтетический черновик');
    return h(ActionForm, {onSubmit: async () => {
      formCalls++;
      await api.mutate('/api/vacancies/71', 'PUT', {title});
      saves++;
    }}, busy => h('input', {value: title, disabled: busy, onChange: e => setTitle(e.target.value)}));
  };
  // Use React's actual state; assert the same controlled value after the failed request.
  const awaitReact = await import('react');
  await act(async () => { root = create(h(Editor)); });
  await act(async () => {
    root.root.findByType('input').props.onChange({target: {value: 'Несохранённое изменение'}});
  });
  await act(async () => {
    submitPromise = root.root.findByType('form').props.onSubmit({preventDefault() {}});
  });
  assert.equal(count('/api/vacancies/71', 'PUT'), 1);
  await answer(next('/api/vacancies/71', 'PUT'), {error: 'Нет прав на изменение'}, 403);
  await act(async () => { await submitPromise; });
  assert.equal(root.root.findByType('input').props.value, 'Несохранённое изменение');
  assert.match(root.root.findByProps({role: 'alert'}).children.join(''), /Нет прав/);
  assert.equal(saves, 0);
  assert.equal(formCalls, 1);
  await stop();

  // 08: успешный, но запоздалый ответ PUT после смены сессии отклоняется;
  // после нового входа нет скрытого повторного POST/PUT.
  const lateWrite = api.mutate('/api/vacancies/71', 'PUT', {title: 'Не повторять'});
  const pendingWrite = next('/api/vacancies/71', 'PUT', 1);
  api.clearCache();
  await answer(pendingWrite, {vacancies: [{id: 71}]});
  await assert.rejects(lateWrite, error => error instanceof api.ApiError && error.status === 409);
  const beforeLoginWrites = count('/api/vacancies/71', 'PUT');
  const newSession = api.request('/api/session');
  await answer(next('/api/session', 'GET', 1), {name: 'Аккаунт А', isAdmin: true});
  await newSession;
  assert.equal(count('/api/vacancies/71', 'PUT'), beforeLoginWrites);

  // 09: пересечение запросов из другой вкладки проверяется тем же контрактом 401;
  // реальное межвкладочное поведение требует отдельного браузерного smoke.
  // 10: первичная проверка сессии обязана вернуть 401 до чтения приватных разделов.
  const initial = api.request('/api/session');
  await answer(next('/api/session', 'GET', 2), {error: 'Требуется вход'}, 401);
  await assert.rejects(initial, error => error instanceof api.ApiError && error.status === 401);
  assert.equal(count('/api/companies/list'), 3, 'После 401 первичной сессии не читаются компании');
  console.log('PASS: сброс приватного кэша, однократный 401, 403 форма и защита от запоздалых записей');
} finally {
  await stop();
  globalThis.fetch = oldFetch;
  if (oldWindow === undefined) delete globalThis.window;
  else globalThis.window = oldWindow;
}
