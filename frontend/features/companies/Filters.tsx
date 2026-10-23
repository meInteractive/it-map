import { useEffect, useRef, useState } from 'react';
import type { Entity } from '../../../shared/contracts';
import { Icon } from '../../components/Common';
export const filterLabels = { rankTop: '1–50', rankBottom: '51–100', rank101to150: '101–150', rank151to200: '151–200', rank201to250: '201–250', rank251to300: '251–300', rankBeyond300: '301+', rankUnranked: 'Без рейтинга', favoriteOnly: 'Избранные', showHidden: 'Показать скрытые', vacanciesOnly: 'С вакансиями', applicationsOnly: 'С откликами' };
export type Filters = Record<keyof typeof filterLabels, boolean>;
export const defaultFilters: Filters = { rankTop: true, rankBottom: true, rank101to150: true, rank151to200: true, rank201to250: true, rank251to300: true, rankBeyond300: true, rankUnranked: true, favoriteOnly: false, showHidden: false, vacanciesOnly: false, applicationsOnly: false };
export const rankStorageKey = 'moscow-it-employers:rank-filter:v1';
export function useFilters() { const [value, setValue] = useState(defaultFilters), [ready, setReady] = useState(false); useEffect(() => { try {
    const stored = JSON.parse(localStorage.getItem(rankStorageKey) || 'null');
    if (stored?.version === 1) {
        const next = { ...defaultFilters };
        for (const key of Object.keys(next) as (keyof Filters)[])
            if (typeof stored[key] === 'boolean')
                next[key] = stored[key];
        setValue(next);
    }
}
catch { } setReady(true); }, []); useEffect(() => { if (ready)
    try {
        localStorage.setItem(rankStorageKey, JSON.stringify({ version: 1, ...value }));
    }
    catch { } }, [ready, value]); return [value, setValue] as const; }
export function matchesEntity(e: Entity, f: Filters) { const r = Number(e.rank) || 0; const key: keyof Filters = r <= 0 ? 'rankUnranked' : r <= 50 ? 'rankTop' : r <= 100 ? 'rankBottom' : r <= 150 ? 'rank101to150' : r <= 200 ? 'rank151to200' : r <= 250 ? 'rank201to250' : r <= 300 ? 'rank251to300' : 'rankBeyond300'; return f[key] && (!e.hidden || f.showHidden) && (!f.favoriteOnly || !!e.favorite) && (!f.vacanciesOnly || !!e.hasVacancies) && (!f.applicationsOnly || !!e.hasApplications); }
export function RankFilter({ value, onChange, count }: {
    value: Filters;
    onChange: (v: Filters) => void;
    count: number;
}) { return <FilterMenu prefix="rank" label={'Места · ' + count} title="Позиция в рейтинге">{Object.entries(filterLabels).map(([k, label]) => <label className="rank-option" key={k}><input type="checkbox" checked={value[k as keyof Filters]} onChange={e => onChange({ ...value, [k]: e.target.checked })}/><span><b>{label}</b></span></label>)}</FilterMenu>; }
export function FilterMenu({ prefix, label, title, children }: {
    prefix: string;
    label: string;
    title: string;
    children: React.ReactNode;
}) {
    const [open, setOpen] = useState(false);
    const container = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (!open) return;
        const outside = (event: Event) => {
            const menu = container.current;
            if (menu && event.target && !menu.contains(event.target as Node)) setOpen(false);
        };
        const close = () => setOpen(false);
        document.addEventListener('pointerdown', outside, true);
        document.addEventListener('focusin', outside, true);
        window.addEventListener('blur', close);
        return () => {
            document.removeEventListener('pointerdown', outside, true);
            document.removeEventListener('focusin', outside, true);
            window.removeEventListener('blur', close);
        };
    }, [open]);
    return <div ref={container} className={prefix + '-filter'} onBlur={e => { if (e.relatedTarget && !e.currentTarget.contains(e.relatedTarget))
    setOpen(false); }} onKeyDown={e => { if (e.key === 'Escape')
    setOpen(false); }}><button type="button" className={prefix + '-filter-button'} aria-expanded={open} onClick={() => setOpen(!open)}><Icon name="filter" className="filter-icon"/><span className="filter-text">{label}</span><Icon name="chevronDown" className="chevron"/></button><div className={prefix + '-filter-menu' + (open ? ' open' : '')} hidden={!open}><div className="rank-filter-title">{title}</div>{children}</div></div>; }
