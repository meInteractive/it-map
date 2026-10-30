import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { applicationConflicts, reconcileApplication, ApplicationConflict } =
  await moduleFor('frontend/features/applications/ApplicationConflict.tsx');
const base = {
  id: 17, revision: 'a'.repeat(64), companyName: 'Тест',
  vacancyId: null, vacancyTitle: '', status: 'Откликнулся', comment: 'Исходный',
  recruiterName: 'Имя', recruiterPhone: '', recruiterEmail: '',
  recruiterContactType: '', applicationSources: ['hh', 'hcm'], history: [],
};
const draft = { ...base, comment: 'Моя правка' };
const current = { ...base, revision: 'b'.repeat(64), recruiterPhone: 'Новый телефон' };
// 1. Изменённые независимо поля сливаются без выбора и без потери ревизии.
assert.deepEqual(applicationConflicts(base, draft, current), []);
const merged = reconcileApplication(base, draft, current);
assert.equal(merged.comment, 'Моя правка');
assert.equal(merged.recruiterPhone, 'Новый телефон');
assert.equal(merged.revision, current.revision);

// 2. Одно конфликтующее поле требует решения; обе стороны поддерживаются.
const overlapping = { ...current, comment: 'Изменено на сервере' };
assert.deepEqual(applicationConflicts(base, draft, overlapping), ['comment']);
assert.throws(() => reconcileApplication(base, draft, overlapping), /Выберите значение/);
assert.equal(reconcileApplication(base, draft, overlapping, { comment: 'draft' }).comment, 'Моя правка');
assert.equal(reconcileApplication(base, draft, overlapping, { comment: 'current' }).comment, 'Изменено на сервере');

// 3. Каждое из двух полей требует отдельного выбора.
const multiDraft = { ...draft, status: 'Офер' };
const multiCurrent = { ...overlapping, status: 'Скрининг' };
assert.deepEqual(applicationConflicts(base, multiDraft, multiCurrent), ['status', 'comment']);
assert.throws(() => reconcileApplication(base, multiDraft, multiCurrent, { status: 'draft' }), /Выберите значение/);
const mixed = reconcileApplication(base, multiDraft, multiCurrent, { status: 'current', comment: 'draft' });
assert.equal(mixed.status, 'Скрининг');
assert.equal(mixed.comment, 'Моя правка');
assert.equal(mixed.recruiterPhone, 'Новый телефон');

// 4. Порядок одного набора источников не создаёт конфликт.
const reordered = { ...base, applicationSources: ['hcm', 'hh'] };
assert.deepEqual(applicationConflicts(base, reordered, current), []);
const newDraft = { ...base, applicationSources: ['hh', 'website'] };
const newCurrent = { ...current, applicationSources: ['hh', 'telegram'] };
assert.deepEqual(applicationConflicts(base, newDraft, newCurrent), ['applicationSources']);
assert.throws(() => reconcileApplication(base, newDraft, newCurrent), /Выберите значение/);
assert.deepEqual(reconcileApplication(base, newDraft, newCurrent,
  { applicationSources: 'draft' }).applicationSources, ['hh', 'website']);
assert.deepEqual(reconcileApplication(base, newDraft, newCurrent,
  { applicationSources: 'current' }).applicationSources, ['hh', 'telegram']);

// 5. Ошибка чтения не применяет результат; повторное чтение требует все решения.
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const oldFetch = globalThis.fetch;
let fail = true, reads = 0, root, resolved;
globalThis.fetch = async url => {
  assert.equal(url, '/api/applications/17');
  reads++;
  return fail ? Response.json({ error: 'Временная ошибка' }, { status: 503 })
    : Response.json({ record: multiCurrent });
};
const button = label => root.root.findAllByType('button').find(b => b.children.join('') === label);
const click = async node => act(async () => node.props.onClick());
try {
  await act(async () => {
    root = create(h(ApplicationConflict, {
      base, draft: multiDraft, onResolved: (...values) => { resolved = values; },
    }));
  });
  await click(button('Загрузить актуальный отклик'));
  assert.equal(reads, 1);
  assert.match(JSON.stringify(root.toJSON()), /Временная ошибка/);
  assert.equal(button('Применить к форме'), undefined);
  fail = false;
  await click(button('Загрузить актуальный отклик'));
  assert.equal(reads, 2);
  assert.equal(button('Применить к форме').props.disabled, true);
  const selects = root.root.findAllByType('select');
  assert.equal(selects.length, 2);
  await act(async () => selects[0].props.onChange({ target: { value: 'current' } }));
  assert.equal(button('Применить к форме').props.disabled, true);
  await act(async () => selects[1].props.onChange({ target: { value: 'draft' } }));
  assert.equal(button('Применить к форме').props.disabled, false);
  await click(button('Применить к форме'));
  assert.equal(resolved[1].status, 'Скрининг');
  assert.equal(resolved[1].comment, 'Моя правка');
  console.log('PASS: слияние отклика, выбор сторон, источники и retry UI');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = oldFetch;
}
