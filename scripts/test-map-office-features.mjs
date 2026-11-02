import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { officeFeatures, validOfficeCoordinates } = await moduleFor('frontend/features/map/geometry.ts');
const make = (name, offices) => ({ name, rank: null, offices, entityType: 'company' });

const empty = officeFeatures([make('Без офисов', []), make('Поле отсутствует', undefined)]);
assert.deepEqual(empty, { type: 'FeatureCollection', features: [] },
  'Отсутствующие офисы не создают фиктивные маркеры');

const entities = [
  make('Корректная', [{ lat: 55.75, lon: 37.61 }]),
  make('Некорректная', [
    { lon: 37.62 }, { lat: 55.76 }, { lat: NaN, lon: 37.63 },
    { lat: 55.77, lon: Infinity }, { lat: '55.78', lon: 37.64 },
    { lat: 100, lon: 37.64 }, { lat: 55.78, lon: 181 },
    { lat: -91, lon: 37.64 }, { lat: 90, lon: 37.64 },
  ]),
  make('Без офисов', []),
];
assert.equal(validOfficeCoordinates(55.75, 37.61), true);
assert.equal(validOfficeCoordinates(100, 37.61), false);
assert.equal(validOfficeCoordinates(55.75, 181), false);
assert.equal(validOfficeCoordinates('55.75', 37.61), false);
const collection = officeFeatures(entities, 'Корректная');
assert.equal(collection.type, 'FeatureCollection');
assert.equal(collection.features.length, 1);
const feature = collection.features[0];
assert.equal(feature.geometry.type, 'Point');
assert.deepEqual(feature.geometry.coordinates, [37.61, 55.75],
  'GeoJSON Point использует [долгота, широта]');
assert.equal(feature.properties.companyName, 'Корректная');
assert.equal(feature.properties.label, 'Корректная');
assert.equal(feature.properties.selected, true);
assert.equal(officeFeatures(entities, 'Другая').features[0].properties.selected, false);
assert.equal(officeFeatures(entities).features[0].properties.selected, false);
console.log('PASS: пустые/некорректные офисы, порядок координат и выбранная компания');
