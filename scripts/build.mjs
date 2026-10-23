import fs from "node:fs";
import path from "node:path";
import { buildDocumentation } from "./documentation.mjs";
import { buildBackupSchema } from "./backup-schema.mjs";

const root = process.cwd();
const template = fs.readFileSync(path.join(root, "worker", "template.js"), "utf8");
const data = fs.readFileSync(path.join(root, "src", "data.json"), "utf8");
const metro = fs.readFileSync(path.join(root, "src", "metro.json"), "utf8");
const mcd3 = fs.readFileSync(path.join(root, "src", "mcd3.json"), "utf8");
const zamoskvoretskaya = fs.readFileSync(path.join(root, "src", "zamoskvoretskaya.json"), "utf8");
const lyublinskoDmitrovskaya = fs.readFileSync(path.join(root, "src", "lyublinsko-dmitrovskaya.json"), "utf8");
const arbatskoPokrovskaya = fs.readFileSync(path.join(root, "src", "arbatsko-pokrovskaya.json"), "utf8");
const pwaIcon192 = fs.readFileSync(path.join(root, "assets", "pwa", "icon-192.png")).toString("base64");
const pwaIcon512 = fs.readFileSync(path.join(root, "assets", "pwa", "icon-512.png")).toString("base64");
const appleTouchIcon = fs.readFileSync(path.join(root, "assets", "pwa", "apple-touch-icon.png")).toString("base64");
const mapLibreJs = fs.readFileSync(path.join(root, "node_modules", "maplibre-gl", "dist", "maplibre-gl-csp.js")).toString("base64");
const mapLibreCss = fs.readFileSync(path.join(root, "node_modules", "maplibre-gl", "dist", "maplibre-gl.css"), "utf8");
const mapLibreWorker = fs.readFileSync(path.join(root, "node_modules", "maplibre-gl", "dist", "maplibre-gl-csp-worker.js")).toString("base64");
fs.mkdirSync(path.join(root, "dist", "server"), { recursive: true });
fs.mkdirSync(path.join(root, "dist", ".openai"), { recursive: true });
fs.writeFileSync(
  path.join(root, "dist", "server", "index.js"),
  template
    .replace("__VACANCY_LOADING_SERVER__", () => fs.readFileSync(path.join(root, "worker/vacancy-loading-server.js"), "utf8"))
    .replace("__CATALOG_SERVER__", () => fs.readFileSync(path.join(root, "worker/catalog-server.js"), "utf8"))
    .replace("__VACANCY_LOADING_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/vacancy-loading-client.js"), "utf8")))
    .replace("__CATALOG_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/catalog-client.js"), "utf8")))
    .replace("__OPENROUTER_SERVER__", () => fs.readFileSync(path.join(root, "worker/openrouter-server.js"), "utf8"))
    .replace("__OPENROUTER_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/openrouter-client.js"), "utf8")))
    .replace("__DOCUMENTATION__", () => JSON.stringify(buildDocumentation(root)))
    .replace("__PROMPTS_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/prompts-client.js"), "utf8")))
    .replace("__RESUME_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/resume-client.js"), "utf8")))
    .replace("__VACANCY_EDITOR_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/vacancy-editor-client.js"), "utf8")))
    .replace("__DOCUMENTATION_CLIENT__", () => JSON.stringify(fs.readFileSync(path.join(root, "worker/documentation-client.js"), "utf8")))
    .replace("__BACKUP_SCHEMA__", () => JSON.stringify(buildBackupSchema(root)))
    .replace("__APP_DATA__", data)
    .replace("__METRO_DATA__", metro)
    .replace("__MCD3_DATA__", mcd3)
    .replace("__ZAMOSKVORETSKAYA_DATA__", zamoskvoretskaya)
    .replace("__LYUBLINSKO_DMITROVSKAYA_DATA__", lyublinskoDmitrovskaya)
    .replace("__ARBATSKO_POKROVSKAYA_DATA__", arbatskoPokrovskaya)
    .replace("__PWA_ICON_192__", pwaIcon192)
    .replace("__PWA_ICON_512__", pwaIcon512)
    .replace("__APPLE_TOUCH_ICON__", appleTouchIcon)
    .replace("__MAPLIBRE_GL_JS__", mapLibreJs)
    .replace("__MAPLIBRE_GL_CSS__", mapLibreCss)
    .replace("__MAPLIBRE_GL_WORKER__", mapLibreWorker),
);
fs.copyFileSync(path.join(root, ".openai", "hosting.json"), path.join(root, "dist", ".openai", "hosting.json"));
const drizzleSource = path.join(root, "drizzle");
if (fs.existsSync(drizzleSource)) {
  fs.cpSync(drizzleSource, path.join(root, "dist", ".openai", "drizzle"), { recursive: true });
}
