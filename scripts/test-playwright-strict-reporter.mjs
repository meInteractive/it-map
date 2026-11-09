import assert from 'node:assert/strict';
import StrictBrowserReporter, {classifyBrowserRun} from './playwright-strict-reporter.mjs';

const executed = [{expectedStatus: 'passed', results: [{status: 'passed'}]}];
const skipped = [{expectedStatus: 'skipped', results: [{status: 'skipped'}]}];
const missing = [{expectedStatus: 'passed', results: []}];
const failed = [{expectedStatus: 'passed', results: [{status: 'failed'}]}];

assert.deepEqual(classifyBrowserRun(executed, 'passed'),
  {total: 1, passed: 1, skipped: 0, status: 'passed'});
for (const tests of [[], skipped, missing, [...executed, ...skipped],
  [...executed, ...missing], [...executed, ...failed]]) {
  assert.equal(classifyBrowserRun(tests, 'passed').status, 'failed',
    'Пустой, частично или полностью пропущенный тестовый прогон не должен быть зелёным');
}
assert.equal(classifyBrowserRun(executed, 'failed').status, 'failed');

const reporter = new StrictBrowserReporter();
const suite = {allTests: () => [...executed, ...missing]};
reporter.onBegin({}, suite);
const previousError = console.error;
try {
  console.error = () => {};
  assert.deepEqual(reporter.onEnd({status: 'passed'}), {status: 'failed'},
    'Строгий репортёр обязан передать failure в Playwright');
} finally {
  console.error = previousError;
}
// Проверяем именно CLI discovery: --list должен показывать имена без запуска браузера.
const listed = new StrictBrowserReporter();
listed.onBegin({}, {allTests: () => missing});
const originalLog = console.log;
process.argv.push('--list');
try {
  console.log = () => {};
  assert.deepEqual(listed.onEnd({status: 'passed'}), {status: 'passed'});
  listed.onBegin({}, {allTests: () => []});
  assert.deepEqual(listed.onEnd({status: 'passed'}), {status: 'failed'});
} finally {
  process.argv.pop();
  console.log = originalLog;
}
console.log('GO-117: строгий reporter отвергает пустые, skipped и частичные прогоны.');
