import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { existsSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import path from 'node:path';

const root = process.cwd();
const p = JSON.parse(readFileSync(path.join(root, 'package.json'), 'utf8'));
const lock = JSON.parse(readFileSync(path.join(root, 'package-lock.json'), 'utf8'));
assert.equal(p.devDependencies.prettier, '3.6.2');
assert.equal(lock.packages[''].devDependencies.prettier, p.devDependencies.prettier);
assert.equal(lock.packages['node_modules/prettier'].version, p.devDependencies.prettier);
assert.equal(p.scripts.format, 'node scripts/format-web.mjs --write');
assert.equal(p.scripts['format:check'], 'node scripts/format-web.mjs --check');
const flow = readFileSync(path.join(root, '.github/workflows/go-ci.yml'), 'utf8');
const job = flow.slice(flow.indexOf('\n  web:\n'));
assert.ok(job.includes('node scripts/test-web-format-guard.mjs'));
assert.ok(job.includes('npm run format:check'));
assert.ok(job.indexOf('npm ci --no-audit --no-fund') < job.indexOf('npm run format:check'));
assert.ok(job.indexOf('npm run format:check') < job.indexOf('npm run build:web'));
const script = readFileSync(path.join(root, 'scripts/format-web.mjs'), 'utf8');
for (const glob of ["'frontend/**/*.{ts,tsx}'", "'shared/**/*.{ts,tsx,mts}'", "'web/main.tsx'", "'vite.web.config.ts'"]) {
  assert.ok(script.includes(glob), 'Обязательная область форматирования отсутствует: ' + glob);
}

const sentinel = path.join(root, 'frontend', '__go114_format_sentinel__.ts');
assert.equal(existsSync(sentinel), false, 'Временный файл уже существует');
const run = (mode) =>
  spawnSync(process.execPath, ['scripts/format-web.mjs', mode], {
    cwd: root,
    encoding: 'utf8',
    timeout: 120000,
    maxBuffer: 4 * 1024 * 1024,
  });
try {
  writeFileSync(sentinel, 'export const go114_sentinel:string="неформатированный";\n');
  let result = run('--check');
  assert.notEqual(result.status, 0, 'Неформатированный новый файл обязан провалить проверку');
  assert.match((result.stdout || '') + (result.stderr || ''), /__go114_format_sentinel__\.ts/);

  result = run('--write');
  assert.equal(result.status, 0, 'Prettier должен отформатировать новый файл');
  const formatted = readFileSync(sentinel, 'utf8');
  assert.notEqual(formatted, 'export const go114_sentinel:string="неформатированный";\n');
  result = run('--check');
  assert.equal(result.status, 0, 'Отформатированный файл должен пройти проверку');
  result = run('--write');
  assert.equal(result.status, 0, 'Повторное форматирование должно проходить');
  assert.equal(readFileSync(sentinel, 'utf8'), formatted, 'Форматирование должно быть идемпотентным');
} finally {
  rmSync(sentinel, { force: true });
}
console.log('GO-114: отрицательный тест, исправление и идемпотентность Prettier подтверждены');
