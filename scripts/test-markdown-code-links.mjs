import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Markdown } = await moduleFor('frontend/components/Markdown.tsx');
const render = value => renderToStaticMarkup(h(Markdown, { value }));

const inline = render('Проверка `run()` внутри строки');
assert.match(inline, /<code>run\(\)<\/code>/);
assert.match(inline, /<p>/);
const multiline = render('```js\nconst value = 1;\nconsole.log(value);\n```');
assert.match(multiline, /<pre><code>/);
assert.match(multiline, /const value = 1;\nconsole\.log\(value\);/);
assert.equal((multiline.match(/<pre>/g) || []).length, 1);

const links = render('[Документация](https://example.test/doc) [Вредная](javascript:alert)');
assert.match(links, /href="https:\/\/example\.test\/doc"/);
assert.equal((links.match(/<a /g) || []).length, 1);
assert.doesNotMatch(links, /href="javascript:/);
assert.match(links, /Вредная/);
console.log('PASS: встроенный и многострочный код, безопасные Markdown-ссылки');
