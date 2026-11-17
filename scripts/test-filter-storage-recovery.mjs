import assert from 'node:assert/strict';
import { createElement as h } from 'react';
import { act, create } from 'react-test-renderer';
import { moduleFor } from './test-support.mjs';

const { useFilters, defaultFilters, rankStorageKey } = await moduleFor('frontend/features/companies/Filters.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

function Probe() {
  const [filters] = useFilters();
  return h('output', { 'data-filters': filters });
}

async function scenario(name, input, expected) {
  const storage = new Map();
  if (input !== null) storage.set(rankStorageKey, input);
  const previous = globalThis.localStorage;
  const writes = [];
  let root;
  try {
    globalThis.localStorage = {
      getItem(key) { return storage.get(key) ?? null; },
      setItem(key, value) { storage.set(key, value); writes.push([key, value]); },
    };
    await act(async () => { root = create(h(Probe)); });
    const observed = root.root.findByType('output').props['data-filters'];
    assert.deepEqual(observed, expected, name);
    assert.ok(writes.length > 0, name + ': настройки сохраняются после инициализации');
    const persisted = JSON.parse(storage.get(rankStorageKey));
    assert.deepEqual(persisted, { version: 2, ...expected }, name + ': допустимый формат сохранён');
  } finally {
    if (root) await act(async () => root.unmount());
    globalThis.localStorage = previous;
  }
}
await scenario('отсутствуют настройки', null, defaultFilters);
await scenario('повреждённый JSON', '{invalid', defaultFilters);
await scenario('неизвестная версия', JSON.stringify({ version: 5, favoriteOnly: true }), defaultFilters);
await scenario('миграция прежнего избранного',
  JSON.stringify({ version: 1, favoriteOnly: true, rankTop: false }),
  { ...defaultFilters, favorites: 'favorites', rankTop: false });
await scenario('миграция прежнего выключенного избранного',
  JSON.stringify({ version: 1, favoriteOnly: false }),
  defaultFilters);
await scenario('три режима: не избранные',
  JSON.stringify({ version: 2, favorites: 'notFavorites', vacanciesOnly: true }),
  { ...defaultFilters, favorites: 'notFavorites', vacanciesOnly: true });
await scenario('неизвестный вариант избранного',
  JSON.stringify({ version: 2, favorites: 'broken', rankBottom: false }),
  { ...defaultFilters, rankBottom: false });
await scenario('недопустимый тип одного значения',
  JSON.stringify({ version: 1, rankTop: false, favoriteOnly: 'true', showHidden: true }),
  { ...defaultFilters, rankTop: false, showHidden: true });
await scenario('частично сохранённые допустимые параметры',
  JSON.stringify({ version: 1, rank101to150: false }),
  { ...defaultFilters, rank101to150: false });

// Изменение одного фильтра сохраняет остальные после размонтирования/возврата.
{
  const previous = globalThis.localStorage;
  const state = new Map();
  let root;
  globalThis.localStorage = {
    getItem(key) { return state.get(key) ?? null; },
    setItem(key, value) { state.set(key, value); },
  };
  function Interactive() {
    const [filters, setFilters] = useFilters();
    return h('output', {
      'data-filters': filters,
      toggle(key, value) { setFilters(current => ({...current, [key]: value})); },
    });
  }
  const output = () => root.root.findByType('output').props;
  try {
    await act(async () => { root = create(h(Interactive)); });
    await act(async () => output().toggle('favorites', 'notFavorites'));
    await act(async () => output().toggle('rankTop', false));
    await act(async () => output().toggle('showHidden', true));
    assert.equal(output()['data-filters'].favorites, 'notFavorites');
    assert.equal(output()['data-filters'].rankTop, false);
    assert.equal(output()['data-filters'].showHidden, true);
    await act(async () => output().toggle('rankTop', true));
    assert.equal(output()['data-filters'].favorites, 'notFavorites', 'Очистка одного фильтра не снимает режим избранного');
    assert.equal(output()['data-filters'].showHidden, true, 'Очистка одного фильтра не снимает скрытые');
    await act(async () => root.unmount()); root = null;
    await act(async () => { root = create(h(Interactive)); });
    assert.equal(output()['data-filters'].favorites, 'notFavorites', 'Возврат к разделу сохраняет активный фильтр');
    assert.equal(output()['data-filters'].rankTop, true);
    assert.equal(output()['data-filters'].showHidden, true);
  } finally {
    if (root) await act(async () => root.unmount());
    globalThis.localStorage = previous;
  }
}
console.log('PASS: v1→v2, три режима избранного, восстановление и независимость фильтров');
