import { useCallback, useEffect, useSyncExternalStore } from 'react';
export class ApiError extends Error {
    constructor(message: string, public status: number, public details?: unknown) { super(message); }
}
interface Snapshot {
    data?: unknown;
    error?: Error;
    loading: boolean;
    stale: boolean;
}
interface Entry {
    snapshot: Snapshot;
    listeners: Set<() => void>;
    pending?: Promise<unknown>;
    generation: number;
    fetchedAt: number;
}
let cacheEpoch = 0;
const entries = new Map<string, Entry>(), empty: Snapshot = { loading: true, stale: true };
function entry(key: string) { let e = entries.get(key); if (!e) {
    e = { snapshot: empty, listeners: new Set(), generation: 0, fetchedAt: 0 };
    entries.set(key, e);
} return e; }
function emit(e: Entry, snapshot: Snapshot) { e.snapshot = snapshot; e.listeners.forEach(fn => fn()); }
export async function request<T>(url: string, options: RequestInit = {}): Promise<T> { const startedEpoch = cacheEpoch; const response = await fetch(url, { credentials: 'same-origin', ...options, headers: { accept: 'application/json', ...(typeof options.body === 'string' ? { 'content-type': 'application/json' } : {}), ...options.headers } }); const data: unknown = response.headers.get('content-type')?.includes('application/json') ? await response.json().catch(() => null) : await response.text(); if (!response.ok) {
    if (response.status === 401 && url !== '/api/session')
        window.dispatchEvent(new Event('session-expired'));
    throw new ApiError(data && typeof data === 'object' && 'error' in data ? String(data.error) : 'Не удалось выполнить запрос.', response.status, data);
} if (options.method === 'POST' && /^\/api\/vacancies\/\d+\/relevance$/.test(url)) {
    if (startedEpoch !== cacheEpoch) throw new ApiError('Сеанс изменился. Обновите список вакансий.', 409);
    applyRelevanceResult(url, data);
} if (options.method && !['GET', 'HEAD'].includes(options.method))
    invalidateMutation(url, options.method); return data as T; }
export function load<T>(key: string): Promise<T> { const e = entry(key); if (e.pending)
    return e.pending as Promise<T>; if (!e.snapshot.stale && e.snapshot.data !== undefined)
    return Promise.resolve(e.snapshot.data as T); const generation = e.generation; emit(e, { ...e.snapshot, loading: true, error: undefined }); const pending = request<T>(key).then(data => { if (generation === e.generation) {
    e.fetchedAt = Date.now();
    emit(e, { data, loading: false, stale: false }); } return data; }, error => { if (generation === e.generation)
    emit(e, { ...e.snapshot, error, loading: false, stale: true }); throw error; }).finally(() => { if (e.pending === pending)
    e.pending = undefined; if (generation !== e.generation && e.listeners.size)
    void load(key).catch(() => { }); }); e.pending = pending; return pending; }
export function invalidate(match: (key: string) => boolean) { for (const [key, e] of entries)
    if (match(key)) {
        e.generation++;
        emit(e, { ...e.snapshot, stale: true });
        if (e.listeners.size && !e.pending)
            void load(key).catch(() => { });
    } }
type CacheEvent = { type: 'reset' } | { type: 'mutation'; path: string };
const cacheListeners = new Set<(event: CacheEvent) => void>();
export function onCacheEvent(listener: (event: CacheEvent) => void) {
    cacheListeners.add(listener);
    return () => { cacheListeners.delete(listener); };
}
function notifyCache(event: CacheEvent) { cacheListeners.forEach(listener => listener(event)); }
// A DELETE acknowledgement is authoritative even if the follow-up GET fails.
// Update cached lists, rather than retaining tombstones across restore/session reset.
function removeDeletedApplication(path: string, method?: string) {
    const match = method === 'DELETE' && path.match(/^\/api\/applications\/(\d+)$/);
    if (!match) return;
    const id = Number(match[1]);
    for (const [key, e] of entries) {
        if (key !== '/api/crm/applications' && !key.startsWith('/api/applications?company=')) continue;
        const data = e.snapshot.data;
        if (!data || typeof data !== 'object' || !('records' in data) || !Array.isArray(data.records)) continue;
        const records = data.records.filter(record => record?.id !== id);
        if (records.length !== data.records.length) emit(e, { ...e.snapshot, data: { ...data, records } });
    }
}
function applyRelevanceResult(path: string, data: unknown) {
    const match = path.match(/^\/api\/vacancies\/(\d+)\/relevance$/);
    if (!match) return;
    if (!data || typeof data !== 'object' || !('vacancyId' in data) || !('relevance' in data))
        throw new ApiError('Модель не вернула корректную релевантность.', 502);
    const id = Number(match[1]), value = data.relevance;
    if (data.vacancyId !== id || typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0 || value > 100)
        throw new ApiError('Модель не вернула корректную релевантность.', 502);
    for (const [key, e] of entries) {
        if (!key.startsWith('/api/vacancies')) continue;
        const cached = e.snapshot.data;
        if (!cached || typeof cached !== 'object') continue;
        const next = {...cached} as Record<string, unknown>;
        let changed = false;
        if ('vacancies' in cached && Array.isArray(cached.vacancies)) {
            next.vacancies = cached.vacancies.map(v => {if (v?.id !== id) return v; changed = true; return {...v, relevance: value};});
        }
        if ('vacancy' in cached && cached.vacancy && typeof cached.vacancy === 'object' && 'id' in cached.vacancy && cached.vacancy.id === id) {
            next.vacancy = {...cached.vacancy, relevance: value}; changed = true;
        }
        if (changed) {e.generation++; emit(e, {...e.snapshot, data: next});}
    }
}
export function invalidateMutation(path: string, method?: string) {
    if (/^\/api\/vacancies\/\d+\/relevance$/.test(path)) {
        notifyCache({type:'mutation',path});
        const detail = path.replace('/relevance', '');
        invalidate(key => key === detail);
        return;
    }
    removeDeletedApplication(path, method);
    notifyCache({ type: 'mutation', path }); if (path.endsWith('/cover-letter')) {
    invalidate(key => key === path.replace('/cover-letter', ''));
    return;
} const catalog = /^\/api\/(companies|vacancies|applications|crm|backup)/.test(path); invalidate(key => key === path || ((catalog || path.startsWith('/api/firecrawl/') || path.startsWith('/api/scheduler')) && (key === '/api/apify/hh-sources' || key.startsWith('/api/apify/hh-source?'))) || (catalog && /^\/api\/(companies|agencies|vacancies|applications|crm|tasks|channels)/.test(key)) || (/^\/api\/vacancy-roles/.test(path) && /^\/api\/(vacancy-roles|vacancies|classification)/.test(key)) || (path.startsWith('/api/apify/') && (key.startsWith('/api/apify/')||key.startsWith('/api/firecrawl/')||key.startsWith('/api/scheduler'))) || (path.startsWith('/api/firecrawl/') && (key.startsWith('/api/firecrawl/')||key.startsWith('/api/scheduler'))) || (path.startsWith('/api/scheduler') && (key.startsWith('/api/scheduler')||key.startsWith('/api/firecrawl/'))) || (catalog && key.startsWith('/api/scheduler')) || (catalog && /^\/api\/firecrawl\/(sources|pending)(?:[?]|$)/.test(key)) || (path.startsWith('/api/classification/') && (key==='/api/classification/settings'||key==='/api/classification/jobs'||key.startsWith('/api/scheduler'))) || (/^\/api\/vacancy-(details|import)/.test(path)&&key.startsWith('/api/scheduler')) || ((path.startsWith('/api/firecrawl/')||path.startsWith('/api/vacancy-candidates'))&&key==='/api/classification/jobs') || (/^\/api\/tasks/.test(path) && key.startsWith('/api/tasks')) || (/^\/api\/google-calendar/.test(path) && /^\/api\/(google-calendar|tasks)/.test(key)) || (/^\/api\/channels/.test(path) && /^\/api\/(channels|vacancies)/.test(key)) || (/^\/api\/hiring-contacts/.test(path) && key.startsWith('/api/hiring-contacts')) || (/^\/api\/openrouter/.test(path) && key.startsWith('/api/openrouter')) || (/^\/api\/openai/.test(path) && key.startsWith('/api/openai')) || (/^\/api\/profile/.test(path) && key.startsWith('/api/profile'))); }
export function useApi<T>(key: string | null, {refreshIntervalMs = 0}: {refreshIntervalMs?: number} = {}) { const subscribe = useCallback((listener: () => void) => { if (!key)
    return () => { }; const e = entry(key); e.listeners.add(listener); return () => { e.listeners.delete(listener); }; }, [key]); const snapshot = useCallback(() => key ? entry(key).snapshot : empty, [key]); const s = useSyncExternalStore(subscribe, snapshot, () => empty); useEffect(() => { if (key)
    void load(key).catch(() => { }); }, [key]);
    useEffect(() => {
        if (!key || refreshIntervalMs <= 0 || typeof window === 'undefined' || typeof document === 'undefined') return;
        const refresh = () => {
            // Server imports happen outside this browser's mutation cache.
            // Keep the displayed data during refresh and coalesce focus/timer events.
            if (document.visibilityState === 'hidden' || entry(key).pending) return;
            invalidate(k => k === key);
        };
        if (Date.now() - entry(key).fetchedAt >= refreshIntervalMs) refresh();
        const timer = setInterval(refresh, refreshIntervalMs);
        window.addEventListener('focus', refresh);
        document.addEventListener('visibilitychange', refresh);
        return () => {
            clearInterval(timer);
            window.removeEventListener('focus', refresh);
            document.removeEventListener('visibilitychange', refresh);
        };
    }, [key, refreshIntervalMs]);
    return { data: s.data as T | undefined, error: s.error, loading: s.loading, reload: () => { if (key)
        invalidate(k => k === key); } }; }
export const mutate = <T>(url: string, method: string, body?: unknown) => request<T>(url, { method, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
export function clearCache() { cacheEpoch++; notifyCache({ type: 'reset' }); for (const e of entries.values()) {
    e.generation++;
    emit(e, empty);
} }
