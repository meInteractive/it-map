import { Fragment, useRef, useState, type ReactNode } from 'react';
import type { EntityType } from '../../shared/contracts';
import paths from './icons.json';
export function Icon({ name, className = '' }: {
    name: string;
    className?: string;
}) { const aliases: Record<string, string> = { close: 'x', map: 'mapPin', list: 'list', check: 'checklist', settings: 'settings', maximize: 'focus' }; const markup = (paths as Record<string, string>)[aliases[name] || name] || (paths as Record<string, string>).checklist; return <svg className={'tabler-icon ' + className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: markup }}/>; }
export function State({ loading, error, children }: {
    loading?: boolean;
    error?: Error;
    children: ReactNode;
}) { return error ? <div className="empty error" role="alert">{error.message}</div> : loading ? <div className="empty">Загрузка…</div> : <>{children}</>; }
export function ErrorText({ error }: {
    error: unknown;
}) { return error ? <div className="error" role="alert">{error instanceof Error ? error.message : String(error)}</div> : null; }
export function Field({ label, children }: {
    label: string;
    children: ReactNode;
}) { return <label className="task-form-field"><span>{label}</span>{children}</label>; }
export function Heading({ title, subtitle, children, actionsClassName = 'tasks-heading-actions' }: {
    title: string;
    subtitle: string;
    children?: ReactNode;
    actionsClassName?: string;
}) { return <div className="page-heading"><div><h1>{title}</h1><p>{subtitle}</p></div><div className={actionsClassName}>{children}</div></div>; }
export function EntitySwitch({ value, onChange, channels = false }: {
    value: EntityType;
    onChange: (v: EntityType) => void;
    channels?: boolean;
}) { return <div className="entity-switch" role="group" aria-label="Тип организации">{(['company', 'agency', ...(channels ? ['channel'] : [])] as EntityType[]).map(t => <button type="button" key={t} aria-pressed={value === t} onClick={() => onChange(t)}>{{ company: 'Компании', agency: 'Агентства', channel: 'Каналы' }[t]}</button>)}</div>; }
export function ActionForm({ children, onSubmit, className = 'react-form', lockWhileBusy = false, disabled = false, onBusyChange }: {
    children: (busy: boolean) => ReactNode;
    onSubmit: () => Promise<void>;
    className?: string;
    lockWhileBusy?: boolean;
    disabled?: boolean;
    onBusyChange?: (busy: boolean) => void;
}) {
    const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
    const inFlight = useRef(false);
    const locked = busy || disabled;
    return <form className={className} aria-busy={locked} onSubmit={async e => {
        e.preventDefault();
        // A state update alone does not guard two submissions in the same render.
        if (inFlight.current || disabled) return;
        inFlight.current = true; setBusy(true); setError(undefined);
        try { onBusyChange?.(true); await onSubmit(); }
        catch (err) { setError(err); }
        finally { inFlight.current = false; setBusy(false); onBusyChange?.(false); }
    }}>
        {lockWhileBusy ? <fieldset className="action-form-fields" disabled={locked}>{children(locked)}</fieldset> : children(locked)}
        <ErrorText error={error}/>
    </form>;
}
export function KV({ rows }: {
    rows: [
        string,
        ReactNode
    ][];
}) { return <div className="kv">{rows.map(([label, value]) => <Fragment key={label}><span>{label}</span><span>{value || '—'}</span></Fragment>)}</div>; }
export const text = (v: unknown): string => v == null || v === '' ? '—' : Array.isArray(v) ? v.join('; ') : String(v);
export const object = (v: unknown): Record<string, unknown> => v && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {};
export function safeUrl(v: unknown) { if (typeof v !== 'string')
    return undefined; try {
    const u = new URL(v);
    return ['http:', 'https:', 'mailto:', 'tel:'].includes(u.protocol) ? v : undefined;
}
catch {
    return undefined;
} }
export function Links({ value }: {
    value: unknown;
}) { return <div className="source-links">{Object.entries(object(value)).filter(([, v]) => safeUrl(v)).map(([k, v]) => <a key={k} className="source" href={safeUrl(v)} target="_blank" rel="noopener noreferrer">{k}</a>)}</div>; }
export function dateLabel(value: string | null | undefined): string {
    if (!value) return '—';
    // D1 CURRENT_TIMESTAMP has no offset, but is always UTC. Keep explicit ISO
    // offsets unchanged instead of letting the browser interpret SQLite as local.
    const timestamp = /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?$/.test(value)
        ? value.replace(' ', 'T') + 'Z' : value;
    const parsed = new Date(timestamp);
    return Number.isNaN(parsed.getTime()) ? '—' : parsed.toLocaleString('ru-RU', {
        timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short',
    });
}
export const moscowParts = (value: Date) => Object.fromEntries(new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(value).map(p => [p.type, p.value]));
export function salary(range: unknown) { if (!Array.isArray(range) || !range.some(Number))
    return '—'; const f = (v: unknown) => Number(v) ? Math.round(Number(v) / 1000).toLocaleString('ru-RU') : ''; return range[0] && range[1] ? f(range[0]) + '–' + f(range[1]) + ' тыс. ₽' : range[0] ? 'от ' + f(range[0]) + ' тыс. ₽' : 'до ' + f(range[1]) + ' тыс. ₽'; }
export const rank = (v: number | null | undefined) => v && v > 0 ? String(v) : '—';
