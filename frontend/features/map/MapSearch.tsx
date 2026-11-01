import { useEffect, useState } from 'react';
import { useApi } from '../../api/client';
import { ErrorText } from '../../components/Common';
import type { Entity, Navigation } from '../../../shared/contracts';
import { mapCompanyLabel } from './labels';
import {normalizeCompanySearch} from '../companies/companySearch';

interface SearchResult {
    matches: { name: string; sector: string }[];
}

// Search supplies suggestions only. Map offices and counts follow the local filters.
export function MapSearch({ query, entities, nav, onQueryChange }: {
    query: string;
    onQueryChange?: (query: string) => void;
    entities: Entity[];
    nav: Navigation;
}) {
    const term = normalizeCompanySearch(query);
    const [search, setSearch] = useState('');
    const [dismissedQuery, setDismissedQuery] = useState<string | null>(null);
    useEffect(() => {
        setDismissedQuery(current => current === term ? current : null);
        if (!term) return;
        const dismiss = (event: MouseEvent) => {
            const target = event.target as Element | null;
            if (!target?.closest?.('.search, .results')) setDismissedQuery(term);
        };
        document.addEventListener('click', dismiss);
        return () => document.removeEventListener('click', dismiss);
    }, [term]);
    const dismissed = dismissedQuery === term;
    function selectCompany(name: string) {
        // Application owns the unsaved-edit confirmation. Do not alter search on cancel.
        if (nav.openCompany(name, 'company') === false) return;
        setDismissedQuery(onQueryChange ? normalizeCompanySearch(name) : term);
        onQueryChange?.(name);
    }
    useEffect(() => {
        const timer = setTimeout(() => setSearch(term), 180);
        return () => clearTimeout(timer);
    }, [term]);
    const result = useApi<SearchResult>(!dismissed && term && term === search
        ? '/api/companies/search?q=' + encodeURIComponent(search) : null);
    if (!term || dismissed) return null;
    const pending = term !== search || result.loading;
    const matches = new Map(result.data?.matches.map(company => [company.name, company]));
    const found = entities.filter(company => matches.has(company.name)).slice(0, 12);
    return <div className="results show" aria-label="Результаты поиска компаний" aria-busy={pending}>
        {pending ? <div className="empty" role="status">Поиск…</div>
            : result.error ? <div className="empty"><ErrorText error={result.error}/><button type="button" onClick={result.reload}>Повторить поиск</button></div>
            : found.length ? found.map(company => <button type="button" className="result" key={company.name} onClick={() => selectCompany(company.name)}>
                <b>{mapCompanyLabel(company)}</b>
                <span>{matches.get(company.name)?.sector} · {(company.offices || []).map(office => office.metro).join(', ')}</span>
            </button>) : <div className="empty" role="status">Ничего не найдено в выбранных фильтрах</div>}
    </div>;
}
