// Строгий отчёт Playwright: пустой/частично пропущенный прогон не считается зелёным.
// Только счётчики. Не выводить cookies, URL, payload, stack или персональные данные.
export function classifyBrowserRun(cases, runnerStatus) {
  const total = cases.length;
  const skipped = cases.filter(test =>
    test.expectedStatus === 'skipped' ||
    !Array.isArray(test.results) || test.results.length === 0 ||
    test.results.some(result => result.status === 'skipped')).length;
  const passed = cases.filter(test =>
    Array.isArray(test.results) && test.results.some(result => result.status === 'passed') &&
    !test.results.some(result => result.status === 'skipped')).length;
  return {
    total, passed, skipped,
    status: runnerStatus === 'passed' && total > 0 && skipped === 0 && passed === total
      ? 'passed' : 'failed',
  };
}

export default class StrictBrowserReporter {
  constructor() {
    this.cases = [];
  }

  onBegin(_config, suite) {
    this.cases = suite.allTests();
  }

  onEnd(result) {
    const summary = classifyBrowserRun(this.cases, result.status);
    if (summary.status !== 'passed') {
      console.error(
        'Playwright CI gate failed: total=' + summary.total +
        ', passed=' + summary.passed +
        ', skipped_or_not_executed=' + summary.skipped +
        ', runner=' + result.status
      );
      return {status: 'failed'};
    }
    console.log('Playwright CI gate: ' + summary.passed + '/' + summary.total + ' executed, 0 skipped.');
    return {status: 'passed'};
  }
}
