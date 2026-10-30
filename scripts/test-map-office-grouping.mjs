import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { officeFeatures } = await moduleFor('frontend/features/map/geometry.ts');
const companies = [
  { name: 'Альфа', rank: 1, entityType: 'company', offices: [
    { lat: 55.75, lon: 37.61 }, { lat: 55.76, lon: 37.62 },
  ] },
  { name: 'Бета', rank: 2, entityType: 'company', offices: [
    { lat: 55.75, lon: 37.61 },
  ] },
];
const features = officeFeatures(companies, 'Альфа').features;
assert.equal(features.length, 3, 'Каждый корректный офис создаёт отдельный Feature');
assert.deepEqual(features.map(f => f.properties.companyName), ['Альфа', 'Альфа', 'Бета']);
assert.ok(features.every(f => f.type === 'Feature' && f.geometry.type === 'Point'));
assert.deepEqual(features.map(f => f.properties.selected), [true, true, false]);

// Две компании делят исходную точку; смещение не должно их склеивать.
const coincident = [features[0].geometry.coordinates, features[2].geometry.coordinates];
assert.notDeepEqual(coincident[0], coincident[1], 'Совпадающие офисы должны разойтись');
assert.ok(coincident.every(([lon, lat]) => Number.isFinite(lon) && Number.isFinite(lat)));
assert.ok(coincident.every(([lon, lat]) => Math.abs(lon - 37.61) < 0.001 &&
  Math.abs(lat - 55.75) < 0.001), 'Смещение небольшое и остаётся возле исходной точки');
// Несовпадающий офис сохраняет исходный порядок GeoJSON [долгота, широта].
assert.deepEqual(features[1].geometry.coordinates, [37.62, 55.76]);
assert.deepEqual(officeFeatures(companies, 'Бета').features.map(f => f.properties.selected),
  [false, false, true]);
assert.deepEqual(officeFeatures(companies, 'Неизвестная').features.map(f => f.properties.selected),
  [false, false, false]);
console.log('PASS: отдельные офисы, разнесённые маркеры и выделение выбранной компании');
