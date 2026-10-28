# Sprint 1.1.06: isolated React SPA build

Актуальные инструкции: [запуск/деплой](../user/deployment/README.md), [подтверждённые browser-проверки](BROWSER_SMOKE.md). Историческое сообщение о недоступной npm-сборке ниже относится к прежней offline-среде; сборка и синтетические desktop/mobile проверки впоследствии прошли. Полная миграция/API parity по-прежнему не завершена.

The original React application remains in `frontend/` and `shared/`.
`npm run build:web` uses `vite.web.config.ts` to build it from `web/main.tsx`
into **ignored** `dist/web/`. This is a separate build from the legacy
Cloudflare/Vinext Worker and does not start or invoke a Site Automation.

Use Node 24 and `npm ci && npm run build:web` on a network-enabled build
machine. `WEB_DIST_DIR=/absolute/path/to/dist/web` instructs Go to serve the
compiled SPA from an isolated static directory. `/api/`, `/auth`, `/logout`,
`/mcp` are always handled by Go, never by the SPA fallback. Missing assets
return 404. When no frontend build exists, the site responds 503 rather than
serving arbitrary files.

**Not yet verified in the offline work environment:** the full npm dependency
tree is not cached, so Vite's output cannot yet be accepted as smoke-tested.
Once dependencies are installed, inspect network calls and confirm that the
React login screen, routes, service worker, map resources and chunk loading
work with the Go server. The API currently returns 503 until its functionality
is migrated.
