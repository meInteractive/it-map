import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

// S11-B03-18/19: синтетическая React-форма и общий navigation guard.
// Проверяем живые handlers, а не текст исходников и не SQL/provider.
const { CompanyApplications } = await moduleFor('frontend/features/applications/Applications.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
const oldWindow = globalThis.window;
const oldConfirm = globalThis.confirm;
const companyName = 'Синтетическая компания';
const records = [1, 2].map((id) => ({
  id, revision: String(id).repeat(64), companyName,
  vacancyId: null, vacancyTitle: '', status: 'Откликнулся',
  comment: 'Комментарий ' + id,
  recruiterName: '', recruiterPhone: '', recruiterEmail: '',
  recruiterContactType: '', applicationSources: [], history: [],
}));
const listeners = new Set();
let approved = false;
let prompts = 0, root;
const flush = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
const cards = () => root.root.findAllByProps({ className: /company-application-card/ });
const card = (index) => root.root.findAllByType('button').filter(
  (x) => String(x.props.className || '').includes('company-application-card'),
)[index];
const comment = () => root.root.findByType('textarea');
const changeComment = async (value) => {
  await act(async () => comment().props.onChange({ target: { value } }));
};
const select = async (index) => {
  await act(async () => card(index).props.onClick());
  await flush();
};

try {
  globalThis.window = {
    addEventListener(type, fn) {
      if (type === 'beforeunload') listeners.add(fn);
    },
    removeEventListener(type, fn) {
      if (type === 'beforeunload') listeners.delete(fn);
    },
  };
  globalThis.confirm = (message) => {
    assert.match(message, /несохранённые изменения/);
    prompts++;
    return approved;
  };
  globalThis.fetch = async (url) => {
    assert.equal(url, '/api/applications?company=' + encodeURIComponent(companyName));
    return Response.json({ records, vacancies: [] });
  };
  await act(async () => { root = create(h(CompanyApplications, { companyName })); });
  await flush();
  assert.ok(card(0), 'Первая запись CRM');
  assert.ok(card(1), 'Вторая запись CRM');

  // Открыть первый отклик. Пока нет правок, маршрут свободен.
  await select(0);
  assert.equal(comment().props.value, 'Комментарий 1');
  assert.equal(listeners.size, 0);

  // Изменение только комментария включает общий guard и beforeunload.
  await changeComment('Личный черновик, не сохранять');
  assert.equal(comment().props.value, 'Личный черновик, не сохранять');
  assert.equal(listeners.size, 1);
  await select(1);
  assert.equal(prompts, 1, 'При переходе запрашивается подтверждение');
  assert.equal(comment().props.value, 'Личный черновик, не сохранять',
    'Отказ не должен уничтожать CRM-черновик');

  // Пользователь явно согласился покинуть изменённую форму.
  approved = true;
  await select(1);
  assert.equal(prompts, 2);
  assert.equal(comment().props.value, 'Комментарий 2');
  assert.equal(listeners.size, 0, 'После перехода old dirty guard снят');

  // Редактирование статуса тоже защищено, возврат значения снимает guard.
  const status = () => root.root.findByProps({ 'aria-label': 'Статус отклика' });
  await act(async () => status().props.onChange({ target: { value: 'Офер' } }));
  assert.equal(listeners.size, 1);
  await act(async () => status().props.onChange({ target: { value: 'Откликнулся' } }));
  assert.equal(listeners.size, 0);
  assert.equal(comment().props.value, 'Комментарий 2');

  console.log('PASS S11-B03/B: CRM comment/status dirty guard, cancelled and confirmed navigation');
} finally {
  if (root) await act(async () => root.unmount());
  if (oldFetch === undefined) delete globalThis.fetch; else globalThis.fetch = oldFetch;
  if (oldWindow === undefined) delete globalThis.window; else globalThis.window = oldWindow;
  if (oldConfirm === undefined) delete globalThis.confirm; else globalThis.confirm = oldConfirm;
}
