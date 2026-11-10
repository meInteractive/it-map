import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../', import.meta.url));
const mode = process.argv[2];
if (mode !== '--check' && mode !== '--write') {
  console.error('Использование: node scripts/format-web.mjs --check|--write');
  process.exit(2);
}

const prettier = path.join(root, 'node_modules/prettier/bin/prettier.cjs');
if (!existsSync(prettier)) {
  console.error('GO-114: Prettier не установлен; выполните npm ci');
  process.exit(1);
}

// Только поддерживаемый React/Vite SPA, без legacy Next/D1, generated и JS-эталонов.
const targets = [
  'frontend/**/*.{ts,tsx}',
  'shared/**/*.{ts,tsx,mts}',
  'web/main.tsx',
  'vite.web.config.ts',
];

const result = spawnSync(process.execPath, [prettier, mode, ...targets], {
  cwd: root,
  stdio: 'inherit',
  timeout: 120000,
});
if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}
process.exit(result.status ?? 1);
