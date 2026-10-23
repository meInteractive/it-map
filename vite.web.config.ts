import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('./web', import.meta.url));
const appRoot = fileURLToPath(new URL('.', import.meta.url));
// Explicitly exclude Cloudflare/Vinext: this is a standalone client-side SPA.
export default defineConfig({
  root,
  publicDir: fileURLToPath(new URL('./public', import.meta.url)),
  plugins: [react()],
  build: { outDir: fileURLToPath(new URL('./dist/web', import.meta.url)), emptyOutDir: true,
           sourcemap: false, manifest: false },
  server: { host: '127.0.0.1', port: 5173, fs: { allow: [appRoot] },
    proxy: {
      '/api': 'http://127.0.0.1:8080',
      '/auth': 'http://127.0.0.1:8080',
      '/logout': 'http://127.0.0.1:8080',
      '/mcp': 'http://127.0.0.1:8080',
    },
  },
});
