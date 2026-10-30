import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { effectivePublicationDate } = await moduleFor('shared/publication-period.ts');
const { publicationLabel } = await moduleFor('frontend/features/vacancies/model.ts');

const vacancy = fields => ({
  id: 1, title: 'Синтетическая вакансия', companyName: 'Компания',
  entityType: 'company', publishedAt: null, loadedAt: null, createdAt: null, ...fields,
});
const cases = [
  [{ publishedAt: '2026-10-03', loadedAt: '2026-10-10', createdAt: '2026-10-15' },
    '2026-10-03', '03.10.2026'],
  [{ publishedAt: null, loadedAt: '2026-10-10', createdAt: '2026-10-15' },
    '2026-10-10', '~ 10.10.2026'],
  [{ publishedAt: 'неверная дата', loadedAt: '2024-02-29', createdAt: '2026-10-15' },
    '2024-02-29', '~ 29.02.2024'],
  [{ publishedAt: null, loadedAt: null, createdAt: '2026-10-15' },
    '2026-10-15', '~ 15.10.2026'],
  [{ publishedAt: '2025-02-29', loadedAt: '2026-02-30', createdAt: '2024-02-29' },
    '2024-02-29', '~ 29.02.2024'],
  [{ publishedAt: null, loadedAt: 'invalid', createdAt: '' }, null, '—'],
  [{ publishedAt: null, loadedAt: null, createdAt: null }, null, '—'],
];
for (const [dates, expected, label] of cases) {
  const input = vacancy(dates);
  const result = effectivePublicationDate(input);
  const iso = result && [result.year, String(result.month).padStart(2, '0'),
    String(result.day).padStart(2, '0')].join('-');
  assert.equal(iso, expected, 'Приоритет publishedAt → loadedAt → createdAt');
  assert.equal(publicationLabel(input), label, 'Приблизительная дата только при fallback');
}
console.log('PASS: приоритет даты публикации, резервные даты и подпись приблизительности');
