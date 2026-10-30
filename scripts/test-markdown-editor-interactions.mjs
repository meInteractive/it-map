import assert from 'node:assert/strict';
import { createElement as h, useState } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { MarkdownEditor } = await moduleFor('frontend/components/Markdown.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const changes = [];
let root;
function ControlledEditor() {
  const [value, setValue] = useState('😀');
  return h(MarkdownEditor, {
    value, limit: 2, label: 'Синтетическая заметка',
    onChange(next) { changes.push(next); setValue(next); },
  });
}
const textarea = () => root.root.findByType('textarea');
const previewButton = () => root.root.findByProps({ className: 'md-tool preview-tool' });
const count = () => root.root.findAll(n => typeof n.props.className === 'string' &&
  n.props.className.split(' ').includes('count'))[0];
const change = async value => act(async () => {
  textarea().props.onChange({ target: { value } });
});

try {
  await act(async () => { root = create(h(ControlledEditor)); });

  // 16, 19. onChange реально обновляет controlled value; emoji — один символ.
  assert.equal(textarea().props.value, '😀');
  assert.match(count().children.join(''), /1 \/ 2/);
  await change('😀x');
  assert.equal(changes.at(-1), '😀x');
  assert.equal(textarea().props.value, '😀x');
  assert.match(count().children.join(''), /2 \/ 2/);

  // 20. На границе валидно, после превышения инвалидно.
  assert.equal(textarea().props['aria-invalid'], false);
  await change('😀xy');
  assert.equal(textarea().props['aria-invalid'], true);
  assert.ok(count().props.className.includes('error'));
  assert.match(count().children.join(''), /3 \/ 2/);
  await change('😀x');
  assert.equal(textarea().props['aria-invalid'], false);

  // 17–18. Предпросмотр сохраняет текст и отключает кнопки форматирования.
  await act(async () => { previewButton().props.onClick(); });
  assert.equal(root.root.findAllByType('textarea').length, 0);
  const preview = root.root.findByProps({ 'aria-label': 'Предпросмотр: Синтетическая заметка' });
  assert.match(JSON.stringify(preview.toJSON()), /😀x/);
  const tools = root.root.findAllByType('button').filter(b => b.props.title);
  assert.equal(tools.length, 5);
  assert.ok(tools.every(b => b.props.disabled === true));
  assert.equal(previewButton().props['aria-pressed'], true);
  await act(async () => { previewButton().props.onClick(); });
  assert.equal(previewButton().props['aria-pressed'], false);
  assert.equal(textarea().props.value, '😀x');
  assert.ok(root.root.findAllByType('button').filter(b => b.props.title)
    .every(b => b.props.disabled === false));
  console.log('PASS: MarkdownEditor, ввод, предпросмотр, emoji и лимит');
} finally {
  if (root) await act(async () => root.unmount());
}
