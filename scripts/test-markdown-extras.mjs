import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { moduleFor } from './test-support.mjs';

const { Markdown } = await moduleFor('frontend/components/Markdown.tsx');
const render = value => renderToStaticMarkup(h(Markdown, { value }));

// 11. Жирное и курсивное начертание — существующий inline-парсер.
const emphasis = render('**Жирный** и *курсивный* текст');
assert.match(emphasis, /<strong>Жирный<\/strong>/);
assert.match(emphasis, /<em>курсивный<\/em>/);

// 12. Нумерованный список не теряет порядок элементов.
const ordered = render('1. Первый\n2. Второй\n3. Третий');
assert.match(ordered, /<ol><li>Первый<\/li><li>Второй<\/li><li>Третий<\/li><\/ol>/);

// 13. Стандартная цитата.
assert.match(render('> Синтетическая цитата'), /<blockquote>Синтетическая цитата<\/blockquote>/);

// 14. Таблица сохраняет порядок столбцов и ячеек в каждой строке.
const tabular = render('| Имя | Статус |\n| --- | --- |\n| Альфа | Готово |\n| Бета | Ожидает |');
assert.match(tabular, /<thead><tr><th>Имя<\/th><th>Статус<\/th><\/tr><\/thead>/);
assert.match(tabular, /<tbody><tr><td>Альфа<\/td><td>Готово<\/td><\/tr><tr><td>Бета<\/td><td>Ожидает<\/td><\/tr><\/tbody>/);

// 15. Локальный файл README.md поддержан, произвольный относительный путь — нет.
const links = render('[Справка](README.md) и [Внутренний путь](docs/readme.md)');
assert.equal((links.match(/<a\b/g) || []).length, 1);
assert.match(links, /href="README\.md"/);
assert.match(links, />Справка<\/a>/);
assert.doesNotMatch(links, /href="docs\/readme\.md"/);
assert.match(links, /Внутренний путь/);

console.log('PASS: выделение, нумерация, цитата, таблица и локальные ссылки Markdown');
