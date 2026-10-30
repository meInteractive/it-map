import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { distanceKm } = await moduleFor('frontend/features/map/geometry.ts');
const eps = 1e-10;
const moscow = [55.7558, 37.6176];
const other = [55.7512, 37.6253];
assert.ok(Math.abs(distanceKm(...moscow, ...moscow)) < eps,
  'Точка до самой себя должна иметь нулевое расстояние');
const forward = distanceKm(...moscow, ...other);
const backward = distanceKm(...other, ...moscow);
assert.ok(forward > 0 && Number.isFinite(forward), 'Расстояние должно быть конечным и положительным');
assert.ok(Math.abs(forward - backward) < eps, 'A → B и B → A симметричны');

// Дуга в один градус по экватору при радиусе Земли 6371 км ≈ 111,195 км.
const equator = distanceKm(0, 0, 0, 1);
assert.ok(Math.abs(equator - 111.195) < 0.01,
  `Ожидалось ≈111,195 км; получено ${equator}`);
console.log('PASS: нуль, симметрия и известная экваториальная дуга distanceKm');
