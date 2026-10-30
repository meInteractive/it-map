import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { vacancySalary } = await moduleFor('frontend/features/vacancies/model.ts');
const vacancy = {
  id: 71, title: 'Синтетическая вакансия', companyName: 'Работодатель',
  entityType: 'company', level: 'Senior', salaryFrom: null, salaryTo: null,
};
// Существующий PR #144 уже проверяет две границы и приоритет зарплаты вакансии.
// Здесь только два недостающих fallback-сценария с одной границей компании.
assert.deepEqual(vacancySalary(vacancy, [{ name: 'Работодатель', senior: [150000, 0] }]), {
  value: '~ от 150к', estimated: true,
});
assert.deepEqual(vacancySalary(vacancy, [{ name: 'Работодатель', senior: [0, 280000] }]), {
  value: '~ до 280к', estimated: true,
});
console.log('PASS: приблизительная зарплата компании при одной заполненной границе');
