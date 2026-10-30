import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Offices } = await moduleFor('frontend/features/companies/Offices.tsx');
const render = offices => renderToStaticMarkup(h(Offices, {
  entity: { name: 'Синтетическая компания', entityType: 'company', offices },
}));
for (const offices of [undefined, null, []]) {
  const html = render(offices);
  assert.match(html, /office-metro-missing/);
  assert.match(html, /Адрес не указан/);
  assert.doesNotMatch(html, /company-office-item/);
}
const withoutMetro = render([{ address: 'Синтетический адрес', metro: '' }]);
assert.match(withoutMetro, /Синтетический адрес/);
assert.match(withoutMetro, /Метро не указано/);
assert.match(withoutMetro, /company-office-item/);
console.log('PASS: пустые офисы и отсутствующее метро в секции карточки компании');
