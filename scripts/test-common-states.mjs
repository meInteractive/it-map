import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { State, ErrorText } = await moduleFor('frontend/components/Common.tsx');
const child = h('strong', null, 'Данные загружены');
const render = props => renderToStaticMarkup(h(State, props, child));

const loading = render({ loading: true });
assert.match(loading, /Загрузка…/);
assert.doesNotMatch(loading, /Данные загружены/);

const failed = render({ loading: true, error: new Error('Синтетическая ошибка') });
assert.match(failed, /role="alert"/);
assert.match(failed, /Синтетическая ошибка/);
assert.doesNotMatch(failed, /Загрузка…|Данные загружены/);
const standalone = renderToStaticMarkup(h(ErrorText, { error: 'Ошибка запроса' }));
assert.match(standalone, /role="alert"/);
assert.match(standalone, /Ошибка запроса/);

assert.equal(render({ loading: false }), '<strong>Данные загружены</strong>');
assert.equal(renderToStaticMarkup(h(ErrorText, { error: null })), '');
console.log('PASS: State и ErrorText — загрузка, ошибка и готовые данные');
