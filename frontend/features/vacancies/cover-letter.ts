import { useEffect, useSyncExternalStore } from 'react';
import { invalidateMutation, onCacheEvent, ApiError } from '../../api/client';
interface LetterState {
    running: boolean;
    message: string;
    error?: Error;
}
interface Entry {
    value: LetterState;
    listeners: Set<() => void>;
    promise?: Promise<void>;
    controller?: AbortController;
    generation: number;
    completed: boolean;
}
const entries = new Map<number, Entry>(), empty: LetterState = { running: false, message: '' };
function entry(id: number) {
    let e = entries.get(id);
    if (!e) {
        e = { value: empty, listeners: new Set(), generation: 0, completed: false };
        entries.set(id, e);
    }
    return e;
}
function emit(e: Entry, value: LetterState) { e.value = value; e.listeners.forEach(fn => fn()); }
// Only operation status is cached here. Personal/shared letter text belongs to useApi.
onCacheEvent(event => {
    for (const [id, e] of entries) {
        if (event.type !== 'reset' && !event.path.startsWith('/api/backup') &&
            event.path !== '/api/vacancies/' + id) continue;
        e.generation++;
        e.completed = false;
        e.controller?.abort();
        emit(e, { ...empty, running: !!e.promise });
    }
});
export function generateLetter(id: number, { regenerate = false } = {}) {
    const e = entry(id);
    if (e.promise) return e.promise;
    const generation = e.generation, controller = new AbortController();
    e.controller = controller;
    emit(e, { running: true, message: 'Генерируем письмо…' });
    e.promise = (async () => {
        try {
            const response = await fetch('/api/vacancies/' + id + '/cover-letter' + (regenerate ? '/regenerate' : ''), {
                method: 'POST', credentials: 'same-origin', headers: { accept: 'application/x-ndjson' },
                signal: controller.signal
            });
            if (generation !== e.generation) { await response.body?.cancel(); return; }
            if (!response.ok) {
                const data = await response.json().catch(() => ({})) as { error?: string };
                if (generation !== e.generation) return;
                if (response.status === 401) window.dispatchEvent(new Event('session-expired'));
                throw new ApiError(data.error || 'Не удалось создать письмо.', response.status);
            }
            const reader = response.body?.getReader();
            if (!reader) throw new Error('Сервер не вернул ответ.');
            const decoder = new TextDecoder();
            let buffer = '', done = false;
            function process(line: string) {
                if (!line.trim() || generation !== e.generation) return;
                const event = JSON.parse(line);
                if (event.type === 'error') throw new Error(event.error || 'Не удалось создать письмо.');
                if (event.type === 'done') {
                    done = true;
                    e.completed = true;
                    emit(e, { running: false, message: 'Письмо сохранено.' });
                    invalidateMutation('/api/vacancies/' + id + '/cover-letter');
                } else if (event.type === 'progress') {
                    emit(e, { running: true, message: 'Генерируем письмо…' +
                        (event.model ? ' ' + event.model : '') +
                        (event.elapsedSeconds ? ' · ' + event.elapsedSeconds + ' с' : '') });
                }
            }
            for (;;) {
                const chunk = await reader.read();
                buffer += decoder.decode(chunk.value, { stream: !chunk.done });
                let pos;
                while ((pos = buffer.indexOf('\n')) >= 0) {
                    process(buffer.slice(0, pos)); buffer = buffer.slice(pos + 1);
                }
                if (generation !== e.generation) { await reader.cancel(); return; }
                if (chunk.done) break;
            }
            if (buffer.trim()) process(buffer);
            if (!done) throw new Error('Соединение прервано до сохранения письма. Попробуйте ещё раз.');
        } catch (error) {
            if (generation === e.generation)
                emit(e, { running: false, message: '', error: error instanceof Error ? error : new Error(String(error)) });
        } finally {
            e.promise = undefined;
            e.controller = undefined;
            if (generation !== e.generation) emit(e, empty);
        }
    })();
    return e.promise;
}
export function useCoverLetter(id: number, existing: string, ready = true) {
    const e = entry(id);
    const state = useSyncExternalStore(fn => {
        e.listeners.add(fn); return () => { e.listeners.delete(fn); };
    }, () => e.value, () => empty);
    useEffect(() => {
        if (ready && !existing && !e.completed && !e.value.error && !e.promise) void generateLetter(id);
    }, [id, existing, ready, e, state]);
    return state;
}
