import { execFileSync, spawnSync } from 'node:child_process';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const mode = process.argv[2];
if (mode !== '--check' && mode !== '--write') {
  console.error('Использование: node scripts/format-web.mjs --check|--write');
  process.exit(2);
}

const active = (name) =>
  /^(?:frontend\/.*\.(?:ts|tsx)|shared\/.*\.(?:ts|tsx|mts)|web\/main\.tsx|vite\.web\.config\.ts)$/.test(name);
const git = (args) =>
  execFileSync('git', args, {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 4 * 1024 * 1024,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
const paths = (args) => git(args).split('\0').filter(Boolean);
const eventFile = process.env.GITHUB_EVENT_PATH;
const event = eventFile && existsSync(eventFile) ? JSON.parse(readFileSync(eventFile, 'utf8')) : {};
let base = event.pull_request?.base?.sha;
if (!base && event.before && !/^0+$/.test(event.before)) base = event.before;

if (!base) {
  for (const name of ['refs/remotes/origin/main', 'main']) {
    try {
      base = git(['merge-base', 'HEAD', name]).trim();
      if (base) break;
    } catch {
      // Локальный checkout может не содержать соответствующую ветку.
    }
  }
}
if (!base) {
  if (process.env.CI) {
    console.error('GO-114: неизвестна база git diff в CI; проверка не может считаться успешной');
    process.exit(1);
  }
  base = git(['rev-parse', 'HEAD']).trim();
}

let files;
try {
  files = new Set([
    ...paths(['diff', '--name-only', '--diff-filter=ACMR', '-z', base, 'HEAD']),
    ...paths(['diff', '--name-only', '--diff-filter=ACMR', '-z', 'HEAD']),
    ...paths(['ls-files', '--others', '--exclude-standard', '-z']),
  ]);
} catch (error) {
  console.error('GO-114: невозможно определить изменённые файлы git:', error.message);
  process.exit(1);
}
const candidates = [...files]
  .map((file) => file.replaceAll('\\', '/'))
  .filter((file) => active(file) && existsSync(path.join(root, file)))
  .sort();
if (candidates.length === 0) {
  console.log('GO-114: нет новых или изменённых поддерживаемых TS/TSX-файлов');
  process.exit(0);
}
const prettier = path.join(root, 'node_modules/prettier/bin/prettier.cjs');
if (!existsSync(prettier)) {
  console.error('GO-114: Prettier не установлен; выполните npm ci');
  process.exit(1);
}
console.log('GO-114:', mode === '--check' ? 'проверка' : 'форматирование', candidates.length, 'файлов');
const result = spawnSync(process.execPath, [prettier, mode, ...candidates], {
  cwd: root,
  stdio: 'inherit',
  timeout: 120000,
});
if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}
process.exit(result.status ?? 1);
