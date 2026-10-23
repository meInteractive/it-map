import {FirecrawlCompanyTest} from '../settings/Firecrawl';
import { companyRankLabel } from './labels';
import { useState } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, ErrorText, Icon, KV, Links, State, object, safeUrl, text } from '../../components/Common';
import { Drawer } from '../../components/Drawer';
import { Markdown, MarkdownEditor } from '../../components/Markdown';
import type { Entity, EntityType, Navigation, VacancyDetail } from '../../../shared/contracts';
import { CompanyApplications } from '../applications/Applications';
import Tasks from '../tasks/Tasks';
import { CompanyJobs } from '../vacancies/Vacancies';
import { companySalary, revenue } from './CompanyList';
import Contacts from './Contacts';
const tabInfo = [['summary', 'Работодатель', 'clipboardText'], ['crm', 'Отклики', 'userCheck'], ['tasks', 'Задачи', 'checklist'], ['jobs', 'Вакансии', 'briefcase'], ['reviews', 'Отзывы', 'message'], ['housing', 'Жильё', 'home'], ['offices', 'Офисы и контакты', 'mapPin'], ['revenue', 'Выручка', 'chartBar'], ['vpnAi', 'VPN / AI', 'robot']] as const;
function rows(o: Record<string, unknown>, mapping: Record<string, string>): [
    string,
    string
][] { return Object.entries(mapping).map(([k, label]) => [label, text(o[k])]); }
const sourceLabels: Record<string, string> = { companySite: 'Сайт компании', career: 'Карьера', hh: 'HH.ru', habr: 'Habr', getmatch: 'Getmatch', telegram: 'Telegram', hCareers: 'Эйч', rabota: 'rabota.ru', dreamJob: 'DreamJob', glassDoor: 'GlassDoor', superJob: 'SuperJob', pravdaSotrudnikov: 'Правда Сотрудников', nahjob: 'Nahjob', bossNadzor: 'BossNadzor', yandexMap: 'YandexMap', twoGis: '2GIS', otzovik: 'Otzovik', personAgency: 'PersonAgency', yell: 'Yell' };
export function EntitySources({ value }: {
    value: unknown;
}) { return <Links value={Object.fromEntries(Object.entries(object(value)).map(([k, v]) => [sourceLabels[k] || k, v]))}/>; }
function AgencyNote({ name }: {
    name: string;
}) { const q = useApi<{
    comment: string;
    status: string;
}>('/api/crm?company=' + encodeURIComponent(name)); return <State loading={!q.data && q.loading} error={q.error}>{q.data && <NoteForm key={name} name={name} initial={q.data.comment || ''} status={q.data.status}/>}</State>; }
function NoteForm({ name, initial, status }: {
    name: string;
    initial: string;
    status: string;
}) { const [value, setValue] = useState(initial), [saved, setSaved] = useState(false); return <ActionForm className="agency-note-editor react-form" onSubmit={async () => { await mutate('/api/crm', 'PUT', { companyName: name, comment: value, status }); setSaved(true); }}>{busy => <><label>Персональный комментарий<MarkdownEditor value={value} onChange={v => { setValue(v); setSaved(false); }} limit={1000}/></label><div className="agency-note-actions"><button type="submit" className="crm-save" disabled={busy}>Сохранить</button>{saved && <span>Сохранено</span>}</div></>}</ActionForm>; }
export function agencyRatingValue(value: unknown, max: number, decimal = false): string {
    if (value == null || String(value).trim() === '' || !Number.isFinite(Number(value))) return '—';
    const number = Number(value);
    const formatted = decimal
        ? number.toLocaleString('ru-RU', { minimumFractionDigits: Number.isInteger(number) ? 0 : 1, maximumFractionDigits: 1 })
        : Math.round(number).toLocaleString('ru-RU');
    return formatted + ' / ' + max;
}
export function agencyRankLabel(value: unknown): string {
    const rankValue = Number(value);
    return Number.isInteger(rankValue) && rankValue > 0 ? '№ ' + rankValue : '—';
}
export function AgencyRating({ c }: { c: Entity }) {
    const ratingInputs = { ethics: 'Этика', vacancyHonesty: 'Честность вакансий', feedback: 'Обратная связь', vacancyRelevance: 'Релевантность вакансий', itExpertise: 'IT-экспертиза', clientKnowledge: 'Знание клиента', processOrganization: 'Организация процесса', candidateSupport: 'Поддержка кандидата' };
    return <section className="agency-profile-group agency-rating-group">
        <h3>Рейтинг</h3>
        <KV rows={[
            ['Итоговая оценка', agencyRatingValue(c.score, 100)],
            ['Балл отзывов', agencyRatingValue(c.reviewsScore, 80, true)],
            ['Балл релевантности', agencyRatingValue(c.relevanceScore, 10)],
            ['Балл выручки', agencyRatingValue(c.revenueScore, 10)],
        ]}/>
        <h4>Оценки по отзывам</h4>
        <KV rows={Object.entries(ratingInputs).map(([key, label]) => [label, agencyRatingValue(c[key], 10, true)])}/>
    </section>;
}
export function AgencySummary({ c }: { c: Entity }) {
    const summary = { ...c, rank: agencyRankLabel(c.rank) };
    const groups: [string, Record<string, string>][] = [
        ['Профиль агентства', { sector: 'Отрасль', agencyTypes: 'Тип агентства', functionalSpecializations: 'Функциональная специализация', positionLevels: 'Уровень позиций', roles: 'Подходящие роли / направления' }],
        ['Модель работы', { executiveSearch: 'Executive Search', closedVacancies: 'Закрытые вакансии', vacancyPublicity: 'Публичность вакансий', employerDisclosure: 'Работодатель в вакансии', salaryDisclosure: 'Зарплата в вакансии' }],
        ['Поиск вакансий', { primaryVacancyChannel: 'Основной канал вакансий', publicSourcesQuality: 'Качество публичных источников', vacancyFreshness: 'Актуальность вакансий' }],
        ['Позиция и данные', { rank: 'Место в рейтинге соискателей', confidence: 'Уверенность в данных', risk: 'Риски / что проверить' }],
    ];
    const vacancySources = object(c.vacancySources);
    const sourceMeta = { companySite: 'Сайт компании', hh: 'HH', habr: 'Хабр', getmatch: 'Getmatch', telegram: 'Telegram', dreamJob: 'DreamJob', hCareers: 'Эйч' };
    const sources = Object.entries(sourceMeta).flatMap(([key, label]) => {
        const url = safeUrl(vacancySources[key]);
        return url && /^https?:/i.test(url) ? [{ label, url }] : [];
    });
    return <>
        <div className="agency-profile-groups">
            <AgencyRating c={c}/>
            {groups.map(([title, fields]) => <section className="agency-profile-group" key={title}>
                <h3>{title}</h3><KV rows={rows(summary, fields)}/>
                {title === 'Поиск вакансий' && sources.length > 0 && <div className="vacancy-sources">
                    <h3>Источники вакансий</h3><div className="vacancy-source-links">{sources.map(({ label, url }) => <a key={label} className="vacancy-source-link" href={url} target="_blank" rel="noopener noreferrer">{label} <Icon name="externalLink"/></a>)}</div>
                </div>}
            </section>)}
        </div>
        <AgencyNote name={c.name}/>
    </>;
}
function Reviews({ c, onFull }: {
    c: Entity;
    onFull: () => void;
}) { const review = object(c.reviews); return <><h2>Отзывы {c.entityType === 'agency' ? 'соискателей' : 'сотрудников'}</h2><div className="review-summary-grid">{(['plus', 'minus'] as const).map(k => <article className={'review-summary-card ' + k} key={k}><h3>{k === 'plus' ? 'Плюсы' : 'Минусы'} — кратко</h3><Markdown value={String(review[k + 'Short'] || '')} empty={'Краткое описание ' + (k === 'plus' ? 'плюсов' : 'минусов') + ' пока не добавлено.'}/></article>)}</div><div className="review-sources"><h3>Источники отзывов</h3><EntitySources value={c.reviewSources}/></div><button className="review-full-button" onClick={onFull}>Полный отзыв</button></>; }
export function ReviewSources({ value }: { value: unknown }) {
    const links = Object.entries(object(value)).flatMap(([key, value]) => {
        const url = safeUrl(typeof value === 'string' ? value.trim() : value);
        return url && /^https?:/i.test(url) ? [{ key, label: sourceLabels[key] || key, url }] : [];
    });
    return links.length ? <div className="review-source-links">{links.map(link =>
        <a key={link.key} className="vacancy-source-button" href={link.url} target="_blank" rel="noopener noreferrer">
            {link.label} <Icon name="externalLink"/>
        </a>)}</div> : <div className="review-source-empty">Источники отзывов пока не добавлены.</div>;
}
export function ReviewFacts({ c }: { c: Entity }) {
    const agency = c.entityType === 'agency';
    return <section className="review-drawer-section active review-facts">
        <h2>{agency ? 'Факты об агентстве' : 'Факты о компании'}</h2>
        <KV rows={agency ? [
            ['Место в рейтинге соискателей', agencyRankLabel(c.rank)], ['Выручка', revenue(c.revenue?.amount)]
        ] : [
            ['Work-life balance', text(c.wlb)], ['Балл WLB', c.wlbScore == null ? '—' : c.wlbScore + ' / 100'],
            ['Рейтинг сотрудников', text(c.rating)], ['Отсрочка', text(c.deferment)], ['Бронь от мобилизации', text(c.mobilization)]
        ]}/>
        {agency && <AgencyRating c={c}/>}
    </section>;
}
export function ReviewDrawer({ c, onClose }: { c: Entity; onClose: () => void }) {
    const [tab, setTab] = useState('plus');
    const r = object(c.reviews);
    return <Drawer className="vacancy-drawer review-drawer" title="Полный отзыв" subtitle={c.name} onClose={onClose}>
        <nav className="vacancy-drawer-tabs review-drawer-tabs" role="tablist" aria-label="Разделы полного отзыва">
            {Object.entries({ plus: 'Плюсы', minus: 'Минусы', facts: 'Факты', sources: 'Источники' }).map(([key, label]) =>
                <button className={'review-drawer-tab' + (tab === key ? ' active' : '')} key={key}
                    type="button" role="tab" aria-selected={tab === key} onClick={() => setTab(key)}>{label}</button>)}
        </nav>
        <div className="vacancy-drawer-body">
            <div className="vacancy-drawer-actions vacancy-drawer-primary-actions"><ReviewSources value={c.reviewSources}/></div>
            {tab === 'facts' ? <ReviewFacts c={c}/> : tab !== 'sources' && <section className="review-drawer-section active">
                <Markdown className="vacancy-drawer-md review-drawer-md markdown-content" value={String(r[tab] || '')} empty="Полный текст пока не добавлен."/>
            </section>}
        </div>
    </Drawer>;
}
export default function CompanyDrawer({ name, type, nav, admin, onClose, onCreate, onEdit }: {
    name: string;
    type?: EntityType;
    nav: Navigation;
    admin: boolean;
    onClose: () => void;
    onCreate: (c: Entity) => void;
    onEdit: (v: VacancyDetail, view: EntityType) => void;
}) { const q = useApi<{
    company: Entity;
}>('/api/' + (type === 'agency' ? 'agencies' : 'companies') + '/detail?name=' + encodeURIComponent(name)); const fallback = useApi<{
    company: Entity;
}>(!type && q.error && 'status' in q.error && q.error.status === 404 ? '/api/agencies/detail?name=' + encodeURIComponent(name) : null); const c = q.data?.company || fallback.data?.company, [tab, setTab] = useState('summary'), [review, setReview] = useState(false), [error, setError] = useState<unknown>(); const agency = c?.entityType === 'agency'; async function flag(key: 'favorite' | 'hidden') { try {
    await mutate('/api/crm/' + (key === 'hidden' ? 'hidden-company' : 'favorite'), 'PUT', { companyName: name, [key]: !c?.[key] });
}
catch (e) {
    setError(e);
} } return <><aside className="panel" role="dialog" aria-label={name}><div className="panel-top"><button className="close" onClick={onClose} aria-label="Закрыть карточку"><Icon name="x"/></button><div className="eyebrow"><span className="rank">{companyRankLabel(c?.rank)}</span><span>{text(c?.confidence)}</span></div><h1>{name}</h1><div className="sector">{c ? [c.sector, c.roles].filter(Boolean).join(' · ') : 'Загрузка компании…'}</div>{c && <div className="score-row">{!agency && <div className="score-tooltip" tabIndex={0} aria-label="Итоговая оценка: соответствие компании вашему поиску работы" data-tooltip="Итоговая оценка: роли — 5, отсрочка и IT-аккредитация — 15, отзывы — 20, WLB — 20, зарплаты — 20, оборот — 10, развитие и внедрение ИИ — 10. Без рейтинга сотрудников отзывы получают консервативные 7 баллов."><div className="pill blue"><strong>{text(c.score)}</strong>/100</div></div>}{!agency && <span className="pill green"><Icon name="star"/> <strong>{text(c.rating)}</strong></span>}<span className="pill"><strong>{c.vacancyCount || 0}</strong> вакансий</span><span className="pill revenue">{revenue(c.revenue?.amount)}</span></div>}</div>{c && <div className="tabs" role="tablist">{tabInfo.filter(([k]) => !agency || !['housing', 'vpnAi'].includes(k)).map(([key, label, icon]) => <button key={key} className={'tab' + (tab === key ? ' active' : '')} title={label} aria-label={label} role="tab" aria-selected={tab === key} onClick={() => setTab(key)}><Icon name={icon}/></button>)}</div>}<div className="content"><State loading={!c&&(q.loading||(!type&&'status'in(q.error||{})&&(q.error as {status?:number})?.status===404&&fallback.loading))} error={c?undefined:fallback.error||q.error}>{c && <section className="section active">{tab === 'summary' ? <><div className="summary-heading"><h2>{agency ? 'Агентство' : 'Работодатель'}</h2><div className="summary-heading-actions"><a className="company-apply" hidden={!safeUrl(c.applicationUrl)} href={safeUrl(c.applicationUrl)} target="_blank" rel="noopener noreferrer">Откликнуться <Icon name="externalLink"/></a><button className={'favorite-toggle' + (c.favorite ? ' active' : '')} aria-label="Избранное" aria-pressed={!!c.favorite} onClick={() => void flag('favorite')}><Icon name="star"/></button><button className={'hidden-toggle' + (c.hidden ? ' active' : '')} aria-label="Скрыть компанию" aria-pressed={!!c.hidden} onClick={() => void flag('hidden')}><Icon name="eyeOff"/></button></div></div><ErrorText error={error}/><div className={agency ? 'agency-salary-summary' : undefined}><KV rows={['Middle', 'Senior', 'Lead', 'Head'].map(l => { const key = l.toLowerCase(), value = companySalary(c[key]); return [l, agency && object(c.salaryRangeSource)[key] === 'vacancies' && value !== '— - —' ? '~ ' + value : value]; })}/></div>{agency ? <AgencySummary c={c}/> : <><KV rows={rows(c, { wlb: 'Work-life balance', deferment: 'Отсрочка', mobilization: 'Бронь', applicationEmail: 'Почта для отклика' })}/><div className="note">{text(c.risk)}</div></>}</> : tab === 'crm' ? <CompanyApplications companyName={name}/> : tab === 'tasks' ? <Tasks companyName={name} nav={nav}/> : tab === 'jobs' ? <><CompanyJobs companyName={name} type={c.entityType} nav={nav} admin={admin} onCreate={() => onCreate(c)} onEdit={v => onEdit(v, c.entityType)}/><EntitySources value={c.vacancySources}/>{admin&&<FirecrawlCompanyTest companyId={agency?null:c.id} agencyId={agency?c.id:null} companyName={name}/>}</> : tab === 'reviews' ? <Reviews c={c} onFull={() => setReview(true)}/> : tab === 'housing' ? <><h2>Корпоративная помощь с жильём</h2><KV rows={rows(object(c.housing), { status: 'Статус', form: 'Форма', terms: 'Условия', after: 'При увольнении' })}/><Links value={{ Источник: object(c.housing).source }}/></> : tab === 'offices' ? <><h2>{agency ? 'Как обратиться' : 'Рабочие площадки в Москве'}</h2><Links value={{ [agency ? 'Сайт агентства' : 'Сайт компании']: object(c.vacancySources).companySite }}/>{agency ? <><KV rows={rows(c, { acceptsCv: 'Принимает CV в базу', recommendedContactMethods: 'Рекомендуемый способ контакта', applicationEmail: 'Почта для отклика' })}/><Links value={{ 'Ссылка для отклика': c.applicationUrl }}/><div className="agency-working-guide"><b>Как работать с агентством</b><p>{text(c.workingGuide)}</p></div></> : c.offices?.map((o, i) => <article className="office" key={i}><b>{o.address}</b><div className="meta">Метро: {o.metro}<br />{text(o.role)} · {text(o.status)}</div><Links value={{ 'Источник адреса': o.source }}/></article>)}<Contacts companyName={name} admin={admin}/></> : tab === 'revenue' ? <><h2>{agency ? 'Выручка агентства' : 'Оборот работодателя'}</h2><KV rows={[["Сумма", revenue(c.revenue?.amount)], ...rows(object(c.revenue), { year: 'Год', metric: 'Показатель', legal: 'Юрлицо / юрлица', inn: 'ИНН', composition: 'Состав суммы', coverage: 'Охват', status: 'Статус' })]}/><div className="note">{text(c.revenue?.comment)}</div><Links value={{ 'Источник 1': c.revenue?.source1, 'Источник 2': c.revenue?.source2 }}/></> : <><h2>Развитие и внедрение ИИ</h2><KV rows={[["Балл ИИ", c.aiScore == null ? 'Не оценено · 0 баллов' : text(c.aiScore) + ' / 10']]}/><div className="note">{text(c.aiEvidence)}</div><div className="note">5 баллов — профильные вакансии, пилоты или внедрение ИИ. 10 — значимое ИИ-направление, продукты, системное внедрение или оборудование для ИИ.</div><h2>VPN + зарубежные нейросети</h2><div className="policy-callout">{text(object(c.vpnAi).summary)}</div><KV rows={rows(object(c.vpnAi), { vpn: 'Личный VPN', ai: 'Общая политика', chatgpt: 'ChatGPT', claude: 'Claude', grok: 'Grok', qwen: 'Qwen', deepseek: 'DeepSeek', data: 'Ограничения данных', evidence: 'Подтверждение', checked: 'Проверено' })}/><div className="note"><b>Уточнить у рекрутера / ИБ</b><br />{text(object(c.vpnAi).recruiter)}</div><Links value={{ 'Источник 1': object(c.vpnAi).source1, 'Источник 2': object(c.vpnAi).source2 }}/></>}</section>}</State></div></aside>{review && c && <ReviewDrawer c={c} onClose={() => setReview(false)}/>}</>; }
