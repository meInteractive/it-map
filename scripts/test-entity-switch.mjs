import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { EntitySwitch } = await moduleFor('frontend/components/Common.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const values = [];
let root;
const render = (value, channels) => h(EntitySwitch, {
  value, channels, onChange: next => values.push(next),
});
const buttons = () => root.root.findAllByType('button');
try {
  await act(async () => { root = create(render('company', false)); });
  assert.deepEqual(buttons().map(b => b.children.join('')), ['Компании', 'Агентства']);
  assert.deepEqual(buttons().map(b => b.props['aria-pressed']), [true, false]);
  await act(async () => { buttons()[1].props.onClick(); });
  assert.deepEqual(values, ['agency']);

  await act(async () => { root.update(render('agency', true)); });
  assert.deepEqual(buttons().map(b => b.children.join('')), ['Компании', 'Агентства', 'Каналы']);
  assert.deepEqual(buttons().map(b => b.props['aria-pressed']), [false, true, false]);
  await act(async () => { buttons()[2].props.onClick(); });
  assert.deepEqual(values, ['agency', 'channel']);
  await act(async () => { root.update(render('channel', true)); });
  assert.deepEqual(buttons().map(b => b.props['aria-pressed']), [false, false, true]);
  assert.equal(root.root.findByProps({ role: 'group' }).props['aria-label'], 'Тип организации');
  console.log('PASS: варианты EntitySwitch и доступный выбранный тип');
} finally {
  if (root) await act(async () => root.unmount());
}
