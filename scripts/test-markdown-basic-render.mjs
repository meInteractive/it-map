import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Markdown } = await moduleFor('frontend/components/Markdown.tsx');
const render = (value, props = {}) =>
  renderToStaticMarkup(h(Markdown, { value, ...props }));

const empty = render('');
assert.match(empty, /history-empty/);
assert.match(empty, /Комментарий пока пустой/);
assert.match(render('  ', { empty: 'Нет записей' }), /Нет записей/);
const content = render('## Заголовок\n\nОбычный абзац');
assert.match(content, /<h2>Заголовок<\/h2>/);
assert.match(content, /<p>Обычный абзац<\/p>/);
const list = render('- Первый\n- Второй\n- Третий');
assert.match(list, /<ul>/);
assert.equal((list.match(/<li>/g) || []).length, 3);
assert.ok(list.indexOf('Первый') < list.indexOf('Второй'));
assert.ok(list.indexOf('Второй') < list.indexOf('Третий'));
console.log('PASS: пустой Markdown, заголовки, абзацы и порядок списка');
