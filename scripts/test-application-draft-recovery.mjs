import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { create, act } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { ApplicationForm } = await moduleFor('frontend/features/applications/Applications.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
const initial = {
  id: 31, revision: 'a'.repeat(64), companyName: 'Тестовый работодатель',
  vacancyId: null, vacancyTitle: '', status: 'Откликнулся',
  comment: 'Исходный комментарий', recruiterName: 'Начальное имя',
  recruiterPhone: '', recruiterEmail: '', recruiterContactType: '',
  applicationSources: ['hh'], history: [],
};
const latest = {
  ...initial, revision: 'b'.repeat(64), status: 'Скрининг',
  comment: 'Комментарий с другого устройства', recruiterPhone: '+7 900 000 00 00',
};
const writes = [], deferred = [];
let phase = 'error', root, saved = 0, reads = 0;
globalThis.fetch = async (url, options = {}) => {
  if (options.method === 'PUT') {
    assert.equal(url, '/api/applications/31');
    const payload = JSON.parse(options.body);
    writes.push(payload);
    return new Promise(resolve => deferred.push(resolve));
  }
  assert.equal(url, '/api/applications/31');
  reads++;
  return Response.json({ record: latest });
};
const button = text => root.root.findAllByType('button').find(n => n.children.join('') === text);
const form = () => root.root.findByType('form');
const change = async (node, value) => act(async () =>
  node.props.onChange({ target: { value } }));
const comment = () => root.root.findByType('textarea');
const status = () => root.root.findByProps({ 'aria-label': 'Статус отклика' });
const recruiterName = () => root.root.findAllByType('input').find(x => x.props.maxLength === 160);
const release = async (data, httpStatus = 200) => act(async () => {
  assert.ok(deferred.length);
  deferred.shift()(Response.json(data, { status: httpStatus }));
  await new Promise(resolve => setTimeout(resolve, 0));
});

try {
  await act(async () => {
    root = create(h(ApplicationForm, {
      record: initial, companyName: initial.companyName, onSaved: () => { saved++; },
    }));
  });
  await change(comment(), 'Мой черновик');
  await change(status(), 'Офер');
  await change(recruiterName(), 'Моё имя рекрутера');

  // 16 и 18. Во время отправки блокировка и отсутствие повторного PUT.
  let pending;
  await act(() => {
    const send = form().props.onSubmit;
    pending = send({ preventDefault() {} });
    void send({ preventDefault() {} });
  });
  assert.equal(writes.length, 1);
  assert.equal(form().props['aria-busy'], true);
  assert.equal(writes[0].comment, 'Мой черновик');
  assert.equal(writes[0].status, 'Офер');
  assert.equal(writes[0].recruiterName, 'Моё имя рекрутера');
  await release({ error: 'Сетевая ошибка сохранения' }, 503);
  await act(async () => pending);
  assert.equal(saved, 0);
  assert.equal(comment().props.value, 'Мой черновик');
  assert.equal(status().props.value, 'Офер');
  assert.equal(recruiterName().props.value, 'Моё имя рекрутера');
  assert.match(JSON.stringify(root.toJSON()), /Сетевая ошибка сохранения/);
  assert.equal(form().props['aria-busy'], false);

  // 17. После ошибки новая пользовательская правка участвует в повторном PUT.
  await change(comment(), 'Мой актуальный черновик');
  await act(() => { pending = form().props.onSubmit({ preventDefault() {} }); });
  assert.equal(writes.length, 2);
  assert.equal(writes[1].comment, 'Мой актуальный черновик');
  assert.equal(writes[1].status, 'Офер');
  assert.equal(writes[1].recruiterName, 'Моё имя рекрутера');
  await release({ error: 'Запись изменилась в другой вкладке' }, 409);
  await act(async () => pending);

  // 19. Конфликт ещё не подтягивает server state в полях формы.
  assert.equal(comment().props.value, 'Мой актуальный черновик');
  assert.equal(status().props.value, 'Офер');
  assert.equal(recruiterName().props.value, 'Моё имя рекрутера');
  assert.equal(button('Сохранить').props.disabled, true);
  assert.equal(reads, 0);
  await act(async () => button('Загрузить актуальный отклик').props.onClick());
  assert.equal(reads, 1);
  assert.equal(button('Применить к форме').props.disabled, true);

  // 20. Статус берём из черновика, комментарий с сервера; имя и телефон независимы.
  await change(root.root.findByProps({ 'aria-label': 'Согласовать: Статус отклика' }), 'draft');
  assert.equal(button('Применить к форме').props.disabled, true);
  await change(root.root.findByProps({ 'aria-label': 'Согласовать: Комментарий' }), 'current');
  assert.equal(button('Применить к форме').props.disabled, false);
  await act(async () => button('Применить к форме').props.onClick());
  assert.equal(status().props.value, 'Офер');
  assert.equal(comment().props.value, 'Комментарий с другого устройства');
  assert.equal(recruiterName().props.value, 'Моё имя рекрутера');
  assert.equal(root.root.findAllByType('input').find(x => x.props.maxLength === 64).props.value,
    '+7 900 000 00 00');

  // Применение не записывает ничего автоматически; только явная отправка.
  assert.equal(writes.length, 2);
  await act(() => { pending = form().props.onSubmit({ preventDefault() {} }); });
  assert.equal(writes.length, 3);
  assert.equal(writes[2].revision, latest.revision);
  assert.equal(writes[2].status, 'Офер');
  assert.equal(writes[2].comment, latest.comment);
  assert.equal(writes[2].recruiterName, 'Моё имя рекрутера');
  assert.equal(writes[2].recruiterPhone, latest.recruiterPhone);
  await release({ records: [{ ...latest, ...writes[2] }] });
  await act(async () => pending);
  assert.equal(saved, 1);
  console.log('PASS: черновик сохраняется после 503, повтор, 409 и пофилдное согласование');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = oldFetch;
}
