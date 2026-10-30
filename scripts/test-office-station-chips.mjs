import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Offices, StationChip } = await moduleFor('frontend/features/companies/Offices.tsx');
const html = renderToStaticMarkup(h(Offices, {
  entity: {
    name: 'Синтетическая компания', entityType: 'company',
    offices: [{ address: 'Синтетический адрес', metro: '  Пушкинская ;  Тверская  ; ;  ' }],
  },
}));
assert.match(html, /Синтетический адрес/);
assert.equal((html.match(/class="office-metro-chip"/g) || []).length, 2);
assert.match(html, />Пушкинская<\/span>/);
assert.match(html, />Тверская<\/span>/);
assert.doesNotMatch(html, />\s+Пушкинская|>\s+Тверская|>\s*<\/span>/);
assert.ok(html.indexOf('>Пушкинская<') < html.indexOf('>Тверская<'), 'Порядок станций сохраняется');

const unknown = renderToStaticMarkup(h(StationChip, { label: 'Несуществующая станция Тест' }));
assert.match(unknown, /Несуществующая станция Тест/);
assert.match(unknown, /--station-fill:#64748b/);
assert.match(unknown, /--station-ink:#fff/);
console.log('PASS: отдельные станции, обрезка пробелов и нейтральный StationChip');
