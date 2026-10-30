import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { safeUrl, Links } = await moduleFor('frontend/components/Common.tsx');
const urls = {
  HTTPS: 'https://example.test/profile',
  HTTP: 'http://example.test/info',
  Email: 'mailto:person@example.test',
  Phone: 'tel:+79990001122',
  Script: 'javascript:alert(1)',
  Broken: 'не ссылка',
};
for (const key of ['HTTPS', 'HTTP', 'Email', 'Phone']) {
  assert.equal(safeUrl(urls[key]), urls[key], key);
}
for (const value of [urls.Script, urls.Broken, '', null, 12]) {
  assert.equal(safeUrl(value), undefined);
}
const html = renderToStaticMarkup(h(Links, { value: urls }));
for (const key of ['HTTPS', 'HTTP', 'Email', 'Phone']) {
  assert.match(html, new RegExp('>' + key + '</a>'));
}
assert.equal((html.match(/<a /g) || []).length, 4);
assert.doesNotMatch(html, /javascript:|>Script<|>Broken</);
assert.match(html, /rel="noopener noreferrer"/);
console.log('PASS: безопасные ссылки и отсутствие опасных href');
