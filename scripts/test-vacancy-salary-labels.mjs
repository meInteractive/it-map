import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { vacancySalary } = await moduleFor('frontend/features/vacancies/model.ts');
const format = value => value.toLocaleString('ru-RU');
const make = input => ({
  id: 101, title: 'Синтетическая вакансия', entityType: 'company',
  companyName: 'Тестовая компания', level: 'Senior',
  salaryFrom: null, salaryTo: null, ...input,
});
const companies = [{ name: 'Тестовая компания', senior: [200000, 350000] }];
assert.deepEqual(vacancySalary(make({ salaryFrom: 150000, salaryTo: 250000 }), companies), {
  value: format(150000) + ' — ' + format(250000) + ' ₽', estimated: false,
});
assert.deepEqual(vacancySalary(make({ salaryFrom: 150000 }), companies), {
  value: 'от ' + format(150000) + ' ₽', estimated: false,
});
assert.deepEqual(vacancySalary(make({ salaryTo: 250000 }), companies), {
  value: 'до ' + format(250000) + ' ₽', estimated: false,
});
assert.deepEqual(vacancySalary(make({}), companies), {
  value: '~ 200к - 350к', estimated: true,
});
assert.deepEqual(vacancySalary(make({ level: 'Junior' }), companies), {
  value: 'зарплата не указана', estimated: false,
});
assert.deepEqual(vacancySalary(make({}), []), {
  value: 'зарплата не указана', estimated: false,
});
console.log('PASS: точные границы, ориентир по компании и отсутствие зарплаты');
