import {publicationPeriods,type PublicationPeriod} from '../../../shared/publication-period';
import {VacancyRelevance} from './Relevance';
import {useSectionState} from '../../hooks/useSectionState';
import { useEffect, useRef, useState } from 'react';
import { useApi, mutate, load, request, ApiError } from '../../api/client';
import { ErrorText, Heading, Icon, State, rank, safeUrl } from '../../components/Common';
import type { Entity, EntityType, Navigation, Vacancy, VacancyList, VacancyDeletionResult, VacancyDetail, VacancyDetailResponse, ApplicationCreationContext } from '../../../shared/contracts';
import type { Filters } from '../companies/Filters';
import { comparePublication, compareVacancies, publicationLabel, vacancyDateLabel, displayEntity, employerName, isMisassigned, vacancySalary, visibleVacancy, type VacancyFilters } from './model';
import {vacancyRoleText} from './RoleTree';
export function VacancyActions({ v, entities, admin, nav, flags = false }: {
    v: Vacancy;
    entities: Entity[];
    admin: boolean;
    nav: Navigation;
    flags?: boolean;
}) {
    const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
    const pending = useRef(false), alive = useRef(true);
    const intent = useRef<{id: number; companyName: string; key: string; context?: string} | undefined>(undefined);
    useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
    const site = entities.find(e => e.name === v.companyName)?.applicationUrl;
    async function action(path: string, method: string, body?: unknown) {
        if (pending.current || (path === '/apply' && v.applicationId)) return;
        pending.current = true;
        setBusy(true);
        setError(undefined);
        try {
            let url = '/api/vacancies/' + v.id + path;
            if (path === '/apply') {
                const current = intent.current ??= {id: v.id, companyName: v.companyName, key: crypto.randomUUID()};
                if (!current.context) {
                    const data = await request<ApplicationCreationContext>('/api/applications/create-context');
                    if (!alive.current) return;
                    if (typeof data?.creationContext !== 'string' || !/^ac1\.[A-Za-z0-9_-]{43}$/.test(data.creationContext))
                        throw new ApiError('Не удалось подготовить создание отклика.', 503);
                    current.context = data.creationContext;
                }
                url = '/api/vacancies/' + current.id + '/apply/create';
                body = {companyName: current.companyName, idempotencyKey: current.key, creationContext: current.context};
            }
            const result = await mutate<VacancyDeletionResult>(url, method, body);
            if (method === 'DELETE') {
                if (result.archived) {
                    alert('У вакансии есть связанный отклик или задача, поэтому она помечена как архивная.');
                } else {
                    nav.vacancyDeleted?.(v.id);
                    if (result.preservedApplication || result.preservedTask) {
                        alert('Вакансия удалена окончательно. Связанные отклики и задачи сохранены без привязки к ней.');
                    }
                }
            }
        } catch (e) {
            if (alive.current) setError(e);
        } finally {
            pending.current = false;
            if (alive.current) setBusy(false);
        }
    }
    function remove() {
        const confirmation = v.archived
            ? 'Удалить архивную вакансию «' + v.title + '» окончательно? Связанные отклики и задачи сохранятся, но больше не будут привязаны к этой вакансии.'
            : 'Удалить вакансию «' + v.title + '» вместе со ссылкой и всеми полями? Если к ней привязан отклик или задача, вакансия останется в базе и будет помечена как архивная.';
        if (confirm(confirmation)) void action('', 'DELETE');
    }
    return <>
        <div className="vacancy-row-actions">
            {flags && <>
                <button className={'favorite-toggle vacancy-state-toggle' + (v.favorite ? ' active' : '')} aria-label="Избранная вакансия" aria-pressed={v.favorite} disabled={busy} onClick={() => void action('/state', 'PUT', { favorite: !v.favorite })}><Icon name="star"/></button>
                <button className={'hidden-toggle vacancy-state-toggle' + (v.hidden ? ' active' : '')} aria-label="Скрытая вакансия" aria-pressed={v.hidden} disabled={busy} onClick={() => void action('/state', 'PUT', { hidden: !v.hidden })}><Icon name="eyeOff"/></button>
            </>}
            {safeUrl(site) && <a className="vacancy-source-button" href={safeUrl(site)} target="_blank" rel="noreferrer">Сайт</a>}
            <button className="vacancy-apply" disabled={busy || !!v.applicationId} onClick={() => void action('/apply', 'POST')}>{v.applicationId ? 'Отклик создан' : 'Откликнуться'}</button>
            {admin && <button className="job-delete" title="Удалить вакансию" aria-label={'Удалить вакансию «' + v.title + '»'} disabled={busy} onClick={remove}><Icon name="trash"/></button>}
        </div>
        <ErrorText error={error}/>
    </>;
}
export function VacancyTable({ records, entities, type = 'company', context = false, nav, admin }: {
    records: Vacancy[];
    entities: Entity[];
    type?: EntityType;
    context?: boolean;
    nav: Navigation;
    admin: boolean;
}) { const headers = ['Название', 'Роль', ...(context && type !== 'company' ? [type === 'agency' ? 'Агентство' : 'Канал'] : []), 'Компания', 'Вилка зарплат', 'Уровень', 'Релевантность', 'Статус отклика', 'Дата публикации', 'Действия']; return <table className={"vacancies-table" + (context && type !== "company" ? " with-entity" : "")}><colgroup>{["title", "role", ...(context && type !== "company" ? ["entity"] : []), "company", "salary", "level", "relevance", "status", "date", "actions"].map(key => <col key={key} className={"vacancy-col-" + key}/>)}</colgroup><thead><tr>{headers.map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{records.map(v => { const company = employerName(v), salary = vacancySalary(v, entities, type), roleLabel=vacancyRoleText(v); return <tr key={v.id} className={isMisassigned(v) ? 'vacancy-misassigned' : ''}><td data-label="Вакансия"><button className="vacancy-title-button" onClick={() => nav.openVacancy(v.id, type)}>{v.title}</button>{isMisassigned(v) && <div className="vacancy-misassigned-note">Ошибка привязки</div>}</td><td data-label="Роль"><span className={'vacancy-role' + (!roleLabel ? ' empty' : '')}>{roleLabel || '—'}</span></td>{context && type !== 'company' && <td data-label={type === 'agency' ? 'Агентство' : 'Канал'}><button className="vacancy-company-button" onClick={() => type === 'channel' ? nav.openChannel(v.telegramSources[0].channelId) : nav.openCompany(displayEntity(v, type), 'agency')}>{displayEntity(v, type)}</button></td>}<td data-label="Компания">{v.entityType !== 'channel' && entities.some(e => e.name === company && e.entityType !== 'agency') ? <button className="vacancy-company-button" onClick={() => nav.openCompany(company)}>{company}</button> : <span className="vacancy-company-name">{company || '—'}</span>}</td><td data-label="Зарплата"><span className={salary.estimated ? 'vacancy-salary-estimate' : ''} title={salary.estimated ? 'Оценка по вилке работодателя для указанного уровня; зарплата в вакансии не указана.' : undefined}>{salary.value}</span></td><td data-label="Уровень"><span className={'vacancy-level' + (!v.level ? ' empty' : '')}>{v.level || '—'}</span></td><td data-label="Релевантность"><VacancyRelevance id={v.id} value={v.relevance} title={v.title}/></td><td data-label="Статус"><div className="vacancy-status-stack">{v.archived && <span className="vacancy-archive-badge">Архивная</span>}<span className={"vacancy-status-badge" + (v.applicationId ? " active" : "")}>{v.applicationStatus || 'Нет отклика'}</span></div></td><td data-label="Дата">{publicationLabel(v)}</td><td data-label="Действия" className="vacancy-delete-cell"><VacancyActions v={v} entities={entities} admin={admin} nav={nav}/></td></tr>; })}</tbody></table>; }
export function CompanyJobFacts({ v, entities, type }: { v: Vacancy; entities: Entity[]; type: EntityType }) {
    const salary = vacancySalary(v, entities, type);
    return <div className="job-facts">
        <span className="job-fact"><span className={salary.estimated ? 'vacancy-salary-estimate' : undefined} title={salary.estimated ? 'Оценка по вилке работодателя для указанного уровня; зарплата в вакансии не указана.' : undefined}>{salary.value}</span></span>
        {vacancyRoleText(v) && <span className="job-fact">{vacancyRoleText(v)}</span>}
        {v.level && <span className="job-fact">{v.level}</span>}
        {v.entityType === 'agency' && <><span className="job-fact">Агентство: {v.companyName}</span>{v.realCompanyName && <span className="job-fact">Работодатель: {v.realCompanyName}</span>}</>}
        <span className="job-fact">Релевантность: {v.relevance == null || String(v.relevance) === '' ? '—' : Number(v.relevance) + '%'}</span>
        <span className="job-fact">Опубликована: {publicationLabel(v)}</span>
        {v.loadedAt && <span className="job-fact">Загружена: {vacancyDateLabel(v.loadedAt)}</span>}
        {v.archived && <span className="vacancy-archive-badge">Архивная</span>}
    </div>;
}
export function CompanyJobs({ companyName, type = 'company', nav, admin, onCreate, onEdit }: {
    companyName: string;
    type?: EntityType;
    nav: Navigation;
    admin: boolean;
    onCreate: () => void;
    onEdit: (v: VacancyDetail) => void;
}) {
    const q = useApi<Omit<VacancyList, 'vacancies'> & { vacancies: VacancyDetail[] }>('/api/vacancies?company=' + encodeURIComponent(companyName));
    const [editing, setEditing] = useState<number | null>(null), [editError, setEditError] = useState<unknown>();
    const pending = useRef(false), sequence = useRef(0);
    useEffect(() => {
        pending.current = false;
        setEditing(null);
        setEditError(undefined);
        return () => { sequence.current++; pending.current = false; };
    }, [companyName, type]);
    async function edit(id: number) {
        if (!admin || pending.current) return;
        pending.current = true;
        const current = sequence.current;
        setEditing(id);
        setEditError(undefined);
        try {
            const detail = await load<VacancyDetailResponse>('/api/vacancies/' + id);
            if (current === sequence.current) onEdit(detail.vacancy);
        } catch (error) {
            if (current === sequence.current) setEditError(error);
        } finally {
            if (current === sequence.current) { pending.current = false; setEditing(null); }
        }
    }
    const vacancies = q.data?.vacancies.filter(v => !v.hidden) || [];
    return <>
        <div className="jobs-heading"><h2>Подходящие вакансии</h2><span className="jobs-count">{vacancies.length}</span>
            {admin && <button className="company-application-new" onClick={onCreate}>Добавить вакансию</button>}
        </div>
        <ErrorText error={editError}/>
        <State loading={!q.data && q.loading} error={q.error}>
            {vacancies.length ? vacancies.map(v => <article className="job" key={v.id}>
                <div className="job-header"><button className="vacancy-title-button" onClick={() => nav.openVacancy(v.id, type)}>{v.title}</button>
                    {admin && <div className="job-header-actions"><button type="button" className="job-full" disabled={editing !== null} onClick={() => void edit(v.id)}>{editing === v.id ? 'Загрузка…' : 'Редактировать'}</button></div>}
                </div>
                <CompanyJobFacts v={v} entities={q.data?.entities || []} type={type}/>
                {safeUrl(v.url) && /^https?:/i.test(v.url) && <a href={safeUrl(v.url)} target="_blank" rel="noopener noreferrer">Открыть вакансию <Icon name="externalLink"/></a>}
                <div><span className={'vacancy-status-badge' + (v.applicationId ? ' active' : '')}>{v.applicationStatus || 'Нет отклика'}</span></div>
                <VacancyActions v={v} entities={q.data?.entities || []} admin={admin} nav={nav}/>
                <button className="job-full" onClick={() => nav.openVacancy(v.id, type)}>Полный текст</button>
            </article>) : <div className="empty">Подходящих вакансий пока нет.</div>}
        </State>
    </>;
}
export default function Vacancies({ nav, admin, filters, vacancyFilters, query, onCount, type='company' }: {
    nav: Navigation;
    type?:EntityType;
    admin: boolean;
    filters: Filters;
    vacancyFilters: VacancyFilters;
    query: string;
    onCount: (n: number) => void;
}) { const [period,setPeriod]=useSectionState<PublicationPeriod>('vacancies:period','week');const q = useApi<VacancyList>('/api/vacancies/list?period='+period, {refreshIntervalMs: 60000}), [sort, setSort] = useSectionState('vacancies:sort','company'); const entities = q.data?.entities || [], directory = new Map(entities.map(e => [e.name, e])), general = new Set(q.data?.applicationSummary?.filter(a => !a.vacancyId).map(a => a.companyName)); const rows = (q.data?.vacancies || []).filter(v => visibleVacancy(v, type, directory, filters, vacancyFilters, query,q.data?.roleTree||[])); useEffect(() => onCount(rows.length), [rows.length, onCount]); const comparator = (a: Vacancy, b: Vacancy) => compareVacancies(a, b, sort, type, general); const groups = new Map<string, Vacancy[]>(); rows.forEach(v => { const name = displayEntity(v, type); groups.set(name, [...(groups.get(name) || []), v]); }); const groupRows = [...groups.entries()].sort((a, b) => type === 'channel' ? a[0].localeCompare(b[0], 'ru') : (directory.get(a[0])?.rank || Infinity) - (directory.get(b[0])?.rank || Infinity) || a[0].localeCompare(b[0], 'ru')); return <div className="vacancies-shell"><Heading actionsClassName="vacancies-heading-actions" title={'Вакансии '+({company:'компаний',agency:'агентств',channel:'каналов'}[type])} subtitle="Сохранённые вакансии и статусы откликов"><label className="vacancy-date-control"><span>Дата публикации</span><select className="vacancy-date-select" aria-label="Дата публикации вакансии" value={period} onChange={e=>setPeriod(e.target.value as PublicationPeriod)}>{Object.entries(publicationPeriods).map(([key,title])=><option key={key} value={key}>{title}</option>)}</select></label><div className="vacancy-sort-control"><span className="vacancy-sort-label">Сортировка</span><div className="vacancy-sort-modes">{Object.entries({ company: 'По компаниям', date: 'Сначала свежие', application: 'С откликом', relevance: 'По релевантности' }).map(([k, label]) => <button key={k} className="vacancy-sort-mode" aria-pressed={sort === k} onClick={() => setSort(k)}>{label}</button>)}</div></div><span className="applications-count">{rows.length}</span></Heading><State loading={!q.data && q.loading} error={q.error}><div className="vacancies-groups">{!rows.length ? <div className="vacancies-empty">В выбранных фильтрах вакансий нет.</div> : sort === 'company' ? groupRows.map(([name, records]) => <section className="vacancies-group" key={name}><div className="vacancies-group-head"><div><button className="vacancies-company" onClick={() => type === 'channel' ? nav.openChannel(records[0].telegramSources[0].channelId) : nav.openCompany(name, type)}>{name}</button>{type !== 'channel' && <span className="vacancies-company-rank">{rank(directory.get(name)?.rank)}</span>}{type !== 'company' && <span className="agency-badge">{type === 'agency' ? 'Агентство' : 'Канал'}</span>}</div><span className="jobs-count">{records.length}</span></div><VacancyTable records={[...records].sort(comparePublication)} entities={entities} type={type} nav={nav} admin={admin}/></section>) : <section className="vacancies-group vacancies-flat"><VacancyTable records={[...rows].sort(comparator)} entities={entities} type={type} context nav={nav} admin={admin}/></section>}</div></State></div>; }
