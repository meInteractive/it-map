import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { text, object } = await moduleFor('frontend/components/Common.tsx');

assert.equal(text(null), '—');
assert.equal(text(undefined), '—');
assert.equal(text(''), '—');
assert.equal(text('Тестовая компания'), 'Тестовая компания');
assert.equal(text(['Первая', 'Вторая', 'Третья']), 'Первая; Вторая; Третья');
assert.equal(text([]), '', 'Пустой массив отличается от отсутствующего значения');

const data = { name: 'Синтетическая запись', score: 42 };
assert.deepEqual(object(data), data);
assert.deepEqual(object(null), {});
assert.deepEqual(object(['не объект']), {});
console.log('PASS: значения text и объектный fallback');
