import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Field, Heading, KV, Icon } = await moduleFor('frontend/components/Common.tsx');
const render = (Component, props, ...children) =>
  renderToStaticMarkup(h(Component, props, ...children));

// 1. Подпись и поле находятся внутри одного label.
const field = render(Field, { label: 'Название' }, h('input', { id: 'name', value: 'Тест', readOnly: true }));
assert.match(field, /^<label[^>]*><span>Название<\/span><input[^>]*\/><\/label>$/);
assert.equal((field.match(/<label/g) || []).length, 1);

// 2–3. Заголовок, подзаголовок и действие в переданном контейнере.
const heading = render(Heading, {
  title: 'Работодатели', subtitle: 'Синтетический каталог', actionsClassName: 'test-actions',
}, h('button', { type: 'button' }, 'Создать'));
assert.match(heading, /<h1>Работодатели<\/h1>/);
assert.match(heading, /<p>Синтетический каталог<\/p>/);
assert.match(heading, /class="test-actions"><button type="button">Создать<\/button>/);

// 4. Порядок пар метка/значение сохраняется.
const kv = render(KV, { rows: [
  ['Компания', 'Альфа'], ['Статус', h('strong', null, 'Активна')], ['Офисы', '2'],
] });
assert.match(kv, /<span>Компания<\/span><span>Альфа<\/span>.*<span>Статус<\/span><span><strong>Активна<\/strong><\/span>.*<span>Офисы<\/span><span>2<\/span>/);

// 5. Неизвестное имя использует существующий fallback checklist.
const known = render(Icon, { name: 'filter' });
const fallback = render(Icon, { name: 'checklist' });
const unknown = render(Icon, { name: 'неизвестная-иконка' });
for (const svg of [known, fallback, unknown]) {
  assert.match(svg, /^<svg\b/);
  assert.match(svg, /aria-hidden="true"/);
}
assert.equal(unknown, fallback);
assert.notEqual(known, fallback);
console.log('PASS: Field, Heading, KV и fallback SVG');
