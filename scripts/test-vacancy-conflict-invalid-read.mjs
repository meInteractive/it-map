import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { create, act } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { VacancyConflict } = await moduleFor('frontend/features/vacancies/VacancyConflict.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const prevFetch = globalThis.fetch;
let root, reads = 0, response;
const valid = {
  id: 71, companyName: 'Синтетический работодатель', entityType: 'company',
  title: 'Актуальная вакансия', descriptionMd: '# Сохранённый текст',
  coverLetterMd: 'Сохранённое письмо', coverLetterSubject: 'Тема',
  coverLetterScope: 'personal', telegramSources: [],
};
globalThis.fetch = async url => {
  assert.equal(url, '/api/vacancies/71');
  reads++;
  return Response.json(response);
};
const button = () => root.root.findByType('button');
const read = async () => act(async () => button().props.onClick());
const current = () => root.root.findAllByProps({ 'aria-label': 'Актуальная версия вакансии' });
try {
  // 6. Запрещено читать при disabled.
  await act(async () => { root = create(h(VacancyConflict, { id: 71, disabled: true })); });
  assert.equal(button().props.disabled, true);
  await read();
  assert.equal(reads, 0);

  // 9. Неполный и чужой ответ не отображаются как актуальная запись.
  await act(async () => root.update(h(VacancyConflict, { id: 71, disabled: false })));
  for (const body of [{}, { vacancy: null }, { vacancy: { ...valid, id: 72 } }]) {
    response = body;
    await read();
    assert.equal(current().length, 0);
    assert.match(JSON.stringify(root.toJSON()), /Не удалось прочитать актуальную вакансию/);
  }
  assert.equal(reads, 3);

  // 8 и 10. После некорректного ответа можно повторить GET и прочитать текст и письмо.
  response = { vacancy: valid };
  await read();
  assert.equal(current().length, 1);
  const areas = current()[0].findAllByType('textarea');
  assert.deepEqual(areas.map(a => a.props.value), ['# Сохранённый текст', 'Сохранённое письмо']);
  assert.ok(areas.every(a => a.props.readOnly && !a.props.onChange));
  assert.equal(root.root.findAllByProps({ role: 'alert' }).length, 0);
  assert.equal(reads, 4);
  console.log('PASS: disabled, чужой/пустой ответ, retry и read-only VacancyConflict');
} finally {
  if (root) await act(async () => root.unmount());
  globalThis.fetch = prevFetch;
}
