import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { settingsSections } = await moduleFor('frontend/features/settings/sections.ts');
const { SettingsNavigation } = await moduleFor('frontend/components/SettingsNavigation.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const userSections = [['general', 'Общие'], ['resume', 'Резюме']];
const adminSections = [
  ...userSections, ['roles', 'Роли'], ['prompts', 'Промты'],
  ['parsing', 'Парсинг вакансий'], ['ai', 'Нейросети'], ['docs', 'Документация'],
];

// 21–23. Контракт порядка и прав из реального settingsSections.
assert.deepEqual(settingsSections(false), userSections);
assert.deepEqual(settingsSections(true), adminSections);

const previous = globalThis.document;
const events = new Map();
globalThis.document = {
  addEventListener(type, handler) { events.set(type, handler); },
  removeEventListener(type, handler) {
    if (events.get(type) === handler) events.delete(type);
  },
};
const selected = [];
let root, focusCount = 0, allowClose = false;
const render = admin => h(SettingsNavigation, {
  admin, active: true, tab: 'general',
  onSelect(key) { selected.push(key); return allowClose; },
});
const trigger = () => root.root.findByProps({
  className: 'app-nav-button settings-nav-trigger',
});
const popover = () => root.root.findAllByProps({ id: 'settings-navigation' });
const items = () => popover()[0].findAllByType('button');
const labels = () => items().map(item => item.children.join(''));
const click = async button => act(async () => button.props.onClick({ detail: 0 }));

try {
  await act(async () => {
    root = create(render(false), {
      createNodeMock(node) {
        if (node.type === 'button' && node.props.className?.includes('settings-nav-trigger'))
          return { focus() { focusCount++; } };
        if (node.props.className === 'settings-nav')
          return { contains() { return false; }, querySelectorAll() { return []; } };
        return null;
      },
    });
  });

  // 24. Кнопка открывает и закрывает меню и синхронизирует aria-expanded.
  assert.equal(trigger().props['aria-expanded'], false);
  assert.equal(popover().length, 0);
  await click(trigger());
  assert.equal(trigger().props['aria-expanded'], true);
  assert.deepEqual(labels(), userSections.map(([, label]) => label));
  assert.equal(items()[0].props['aria-current'], 'page');
  await click(trigger());
  assert.equal(trigger().props['aria-expanded'], false);
  assert.equal(popover().length, 0);

  // 25. Отказ onSelect оставляет меню открытым; успех закрывает его.
  await click(trigger());
  await click(items()[1]);
  assert.deepEqual(selected, ['resume']);
  assert.equal(trigger().props['aria-expanded'], true);
  assert.equal(popover().length, 1);
  allowClose = true;
  await click(items()[1]);
  assert.deepEqual(selected, ['resume', 'resume']);
  assert.equal(trigger().props['aria-expanded'], false);
  assert.equal(popover().length, 0);
  assert.equal(focusCount, 1, 'После выбора фокус возвращается на кнопку');

  // Администратор получает весь каталог в порядке settingsSections.
  await act(async () => root.update(render(true)));
  await click(trigger());
  assert.deepEqual(labels(), adminSections.map(([, label]) => label));
  await click(items()[6]);
  assert.deepEqual(selected, ['resume', 'resume', 'docs']);
  assert.equal(trigger().props['aria-expanded'], false);
  console.log('PASS: права разделов, меню и onSelect SettingsNavigation');
} finally {
  if (root) await act(async () => root.unmount());
  assert.equal(events.size, 0, 'Подписка pointerdown удаляется');
  if (previous === undefined) delete globalThis.document;
  else globalThis.document = previous;
}
