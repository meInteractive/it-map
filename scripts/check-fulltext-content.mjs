import { checkVacancyMarkdown } from '../server/services/vacancy-fulltext.js';

const content = 'Обязанности: писать код. Требования: опыт разработки. ' + 'Описание вакансии. '.repeat(20);
const cases = [
  ['valid', content, 200], ['empty', '', 200], ['too_short', 'Обязанности и требования', 404],
  ['status', content, 302], ['captcha', 'CAPTCHA\n' + content, 200],
  ['login', '# Вход\n' + content, 200], ['closed', 'Вакансия закрыта\n' + content, 200],
  ['one_signal', 'Обязанности: ' + 'Писать код. '.repeat(30), 200],
  ['english', 'Responsibilities and requirements. ' + 'Details '.repeat(40), 200],
  ['listing', '# Вакансии\n## Первая\n## Вторая\n## Третья\n' + content, 200],
  ['emoji249', '😀'.repeat(124) + 'x', 200], ['emoji250', '😀'.repeat(125), 200],
  ['late_captcha', 'x'.repeat(3000) + 'CAPTCHA ' + content, 200],
  ['late_signals', 'x'.repeat(12000) + content, 200],
  ['nbsp_trim', '\u00a0'.repeat(10) + 'x'.repeat(249) + '\u00a0', 200],
  ['long_s', 'Reſponſibilitieſ requirements ' + 'x'.repeat(260), 200],
  ['emoji_distance', 'verify' + '😀'.repeat(16) + 'human ' + content, 200],
  ['cr_lines', '# Вакансии\r## Первая\r## Вторая\r## Третья\r' + content, 200],
];
const url = 'https://careers.company.org/jobs/123';
console.log(JSON.stringify(cases.map(([name, markdown, status]) => ({
  name, markdown, status,
  errors: checkVacancyMarkdown(markdown, {sourceUrl: url, finalUrl: url, targetStatus: status}).errors,
}))));
