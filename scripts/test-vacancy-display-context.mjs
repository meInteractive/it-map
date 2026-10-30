import assert from 'node:assert/strict';
import { moduleFor } from './test-support.mjs';

const { displayEntity, vacancyContext } =
  await moduleFor('frontend/features/vacancies/model.ts');
const company = { id: 1, companyName: 'Альфа', entityType: 'company' };
const agency = {
  id: 2, companyName: 'Агентство', entityType: 'agency',
  realCompanyName: 'Работодатель',
};
const channel = {
  id: 3, companyName: 'Вакансия из канала', entityType: 'channel',
  telegramSources: [
    { channelId: 42, channelName: 'Первый канал' },
    { channelId: 43, channelName: 'Второй канал' },
  ],
};

// 26. Компания в соответствующем представлении.
assert.equal(displayEntity(company, 'company'), 'Альфа');
// 27. Агентство и реальный работодатель: разные контексты.
assert.equal(displayEntity(agency, 'agency'), 'Агентство');
assert.equal(displayEntity(agency, 'company'), 'Работодатель');
// 28. Используется только первый источник, пустой список даёт пустую строку.
assert.equal(displayEntity(channel, 'channel'), 'Первый канал');
assert.equal(displayEntity({ ...channel, telegramSources: [] }, 'channel'), '');
assert.equal(displayEntity({ ...channel, telegramSources: [{ channelId: 42 }] }, 'channel'), '');
// 29. Контекст канала включает ID первого источника.
assert.deepEqual(vacancyContext(channel, 'channel'), {
  name: 'Первый канал', type: 'channel', channelId: 42,
});
// 30. При отсутствии имени выбранного представления возвращаем исходную компанию и тип.
assert.deepEqual(vacancyContext({ ...agency, realCompanyName: '' }, 'company'), {
  name: 'Агентство', type: 'agency', channelId: undefined,
});
assert.deepEqual(vacancyContext({ ...channel, telegramSources: [] }, 'channel'), {
  name: 'Вакансия из канала', type: 'channel', channelId: undefined,
});
console.log('PASS: displayEntity и vacancyContext без API и сети');
