import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor, nav } from './test-support.mjs';

// Дополнительная React unit-регрессия к BATCH-03/A (#500): два пользователя, реальный drawer
// и настоящий API cache client. Ни БД, ни provider/network здесь нет.
const ui = await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
const oldDocument = globalThis.document;
const oldWindow = globalThis.window;
let root;
let user = 'first';
const flags = {
  first: { favorite: false, hidden: false },
  second: { favorite: false, hidden: false },
};
const calls = [];
const waiting = [];
const company = {
  name: 'Синтетический работодатель',
  entityType: 'company',
  rank: 12,
  offices: [],
  roles: [],
  vacancyCount: 0,
};

const detail = '/api/companies/detail?name=' + encodeURIComponent(company.name);
const flush = async () => act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 0));
});
const button = (label) =>
  root.root.findAllByType('button').find((node) => node.props['aria-label'] === label);
const current = (label) => {
  const node = button(label);
  assert.ok(node, 'Должна существовать кнопка ' + label);
  return node;
};
const count = (method) => calls.filter((x) => x.method === method).length;
const settle = async (status = 200) => {
  const pending = waiting.shift();
  assert.ok(pending, 'Нужна ожидающая запись');
  if (status === 200) flags[pending.user][pending.key] = pending.value;
  await act(async () => {
    pending.resolve(
      status === 200
        ? Response.json({ ...flags[pending.user] })
        : Response.json({ error: 'Синтетический отказ записи' }, { status }),
    );
  });
  await flush();
};
const mount = async () => {
  await act(async () => {
    root = create(
      h(ui.CompanyDrawer, {
        name: company.name,
        type: 'company',
        nav,
        admin: false,
        onClose() {},
        onCreate() {},
        onEdit() {},
      }),
    );
  });
  await flush();
  assert.ok(current('Избранное'));
};
const unmount = async () => {
  if (root) await act(async () => root.unmount());
  root = null;
  ui.clearCache();
};
const click = async (label) => {
  await act(async () => {
    current(label).props.onClick();
  });
};

try {
  globalThis.document = { activeElement: null, querySelector() { return null; } };
  globalThis.window = {
    addEventListener() {},
    removeEventListener() {},
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
  };
  globalThis.fetch = (url, options = {}) => {
    const method = options.method || 'GET';
    calls.push({ url, method, user });
    if (method === 'GET' && url === detail)
      return Promise.resolve(Response.json({ company: { ...company, ...flags[user] } }));
    if (method === 'GET' && url === '/api/crm?company=' + encodeURIComponent(company.name))
      return Promise.resolve(Response.json({ status: '', comment: '', ...flags[user], history: [] }));
    if (method === 'PUT' && (url.startsWith('/api/crm/favorite?') || url.startsWith('/api/crm/hidden-company?'))) {
      const key = url.startsWith('/api/crm/favorite?') ? 'favorite' : 'hidden';
      const payload = JSON.parse(options.body);
      assert.equal(typeof payload[key], 'boolean');
      const requestUser = user;
      return new Promise((resolve) => waiting.push({
        resolve, user: requestUser, key, value: payload[key],
      }));
    }
    throw new Error('Unexpected synthetic URL: ' + method + ' ' + url);
  };

  await mount();
  assert.equal(current('Избранное').props['aria-pressed'], false);
  assert.equal(current('Скрыть компанию').props['aria-pressed'], false);

  // 03/05: повторный вызов обработчика до React commit не создаёт второй PUT.
  await act(async () => {
    const toggle = current('Избранное');
    toggle.props.onClick();
    toggle.props.onClick();
  });
  assert.equal(count('PUT'), 1);
  assert.equal(current('Избранное').props.disabled, true);
  assert.equal(current('Избранное').props['aria-pressed'], false, 'Нет ложного optimistic success');
  await settle();
  assert.equal(flags.first.favorite, true);
  assert.equal(current('Избранное').props.disabled, false);
  assert.equal(current('Избранное').props['aria-pressed'], true, 'Данные перечитаны после записи');

  // 01/02/10: смена сессии очищает личные флаги, вторая учётная запись их не наследует.
  await unmount();
  user = 'second';
  await mount();
  assert.equal(current('Избранное').props['aria-pressed'], false);
  assert.equal(current('Скрыть компанию').props['aria-pressed'], false);
  await unmount();

  // 03/04/08: повторное открытие первой карточки сохраняет ровно её состояние.
  user = 'first';
  await mount();
  assert.equal(current('Избранное').props['aria-pressed'], true);

  // 06/07: отказ PUT сверяется read-only GET; retry берёт подтверждённые значения.
  await click('Скрыть компанию');
  assert.equal(current('Скрыть компанию').props.disabled, true);
  await settle(503);
  assert.equal(flags.first.hidden, false);
  assert.equal(current('Скрыть компанию').props['aria-pressed'], false);
  assert.equal(current('Скрыть компанию').props.disabled, false);
  await click('Скрыть компанию');
  assert.equal(waiting[0].value, true);
  await settle();
  assert.equal(current('Скрыть компанию').props['aria-pressed'], true);
  assert.equal(flags.first.hidden, true);

  // 09/10: поздний ответ записи старого аккаунта не подтверждает новую сессию.
  await click('Избранное');
  const writeBeforeSwitch = waiting[0];
  assert.equal(writeBeforeSwitch.user, 'first');
  await unmount();
  user = 'second';
  await mount();
  assert.equal(current('Избранное').props['aria-pressed'], false);
  await settle(); // Возвращается запись первой сессии, request() отвергает её epoch.
  assert.equal(current('Избранное').props['aria-pressed'], false);
  assert.equal(flags.second.favorite, false);

  console.log('PASS S11-B03/A: two users, stale double-click, confirmed state, 503/retry, logout race');
} finally {
  await unmount();
  globalThis.fetch = oldFetch;
  globalThis.document = oldDocument;
  globalThis.window = oldWindow;
}
