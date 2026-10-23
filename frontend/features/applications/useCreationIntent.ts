import {useEffect, useRef, useState} from 'react';
import {ApiError, request} from '../../api/client';
import type {ApplicationCreationContext} from '../../../shared/contracts';

export function useCreationIntent(enabled: boolean) {
    const [key] = useState(() => enabled ? crypto.randomUUID() : undefined);
    const context = useRef<string | undefined>(undefined);
    const pending = useRef(enabled);
    const [attempt, setAttempt] = useState(0);
    const [state, setState] = useState<{loading: boolean; ready: boolean; error?: unknown}>({loading: enabled, ready: false});
    useEffect(() => {
        if (!enabled || context.current) return;
        let alive = true;
        pending.current = true;
        setState({loading: true, ready: false});
        // Draft-local and uncached: global cache invalidation must not replace it.
        void request<ApplicationCreationContext>('/api/applications/create-context').then(data => {
            if (typeof data?.creationContext !== 'string' || !/^ac1\.[A-Za-z0-9_-]{43}$/.test(data.creationContext))
                throw new ApiError('Не удалось подготовить создание отклика.', 503);
            if (alive) {
                context.current = data.creationContext;
                setState({loading: false, ready: true});
            }
        }).catch(error => { if (alive) setState({loading: false, ready: false, error}); })
          .finally(() => { if (alive) pending.current = false; });
        return () => { alive = false; };
    }, [enabled, attempt]);
    return {
        ...state,
        retry() {
            // Retry context acquisition only before any POST could be sent.
            if (pending.current || context.current) return;
            pending.current = true;
            setAttempt(value => value + 1);
        },
        value() {
            if (!key || !context.current) throw new ApiError('Дождитесь подготовки создания отклика.', 428);
            return {idempotencyKey: key, creationContext: context.current};
        },
    };
}
