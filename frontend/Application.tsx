'use client';
import { lazy, Suspense, useEffect, useState, useMemo, useRef } from 'react';
import { ApiError, useApi, clearCache } from './api/client';
import { Icon, State } from './components/Common';
import type { EntityType, Navigation, Session, VacancyDetail } from '../shared/contracts';
import { levels } from '../shared/contracts';
import { RankFilter, FilterMenu, useFilters } from './features/companies/Filters';
import { defaultVacancyFilters } from './features/vacancies/model';
import {RoleTreeOptions} from './features/vacancies/RoleTree';
import { confirmNavigation, allowSessionExit } from './hooks/useDirty';
import Tasks from './features/tasks/Tasks';
import {SettingsNavigation} from './components/SettingsNavigation';
import {EntityNavigation} from './components/EntityNavigation';
import {useSectionState} from './hooks/useSectionState';
import {sectionFromPath, sectionPath, rememberLoginDestination, restoreLoginDestination, type Section} from './lib/routes';
import { PanelBoundary } from './components/PanelBoundary';
const Applications = lazy(() => import('./features/applications/Applications')), Vacancies = lazy(() => import('./features/vacancies/Vacancies')), CompanyList = lazy(() => import('./features/companies/CompanyList')), MapSection = lazy(() => import('./features/map/Map')), Settings = lazy(() => import('./features/settings/Settings')), CompanyDrawer = lazy(() => import('./features/companies/CompanyDrawer')), VacancyDrawer = lazy(() => import('./features/vacancies/VacancyDrawer')), VacancyEditor = lazy(() => import('./features/vacancies/VacancyEditor')), ChannelDrawer = lazy(() => import('./features/channels/Channels'));
const sections = [['tasks', 'Задачи', 'checklist'], ['applications', 'Отклики', 'userCheck'], ['vacancies', 'Вакансии', 'briefcase'], ['list', 'Список', 'list'], ['map', 'Карта', 'mapPin'], ['settings', 'Настройки', 'settings']] as const;
export function SectionSearch({ section, value, onChange }: {
    section: string;
    value: string;
    onChange: (value: string) => void;
}) {
    const vacancy = section === 'vacancies';
    const field = <label className={'search' + (vacancy ? ' vacancy-search' : '')}
        aria-label={vacancy ? 'Поиск вакансий' : 'Поиск компаний'}><Icon name="search"/>
        <input value={value} placeholder={vacancy ? 'Название вакансии…' : section === 'list' ? 'Название компании…' : 'Компания, метро, вакансия, VPN, AI…'}
            autoComplete="off" onChange={e => onChange(e.target.value)}/>
    </label>;
    return vacancy ? <div className="vacancy-search-wrap">{field}</div> : field;
}
function Login() { const [error, setError] = useState(false); useEffect(() => { setError(location.pathname === '/auth'); if (typeof sessionStorage !== 'undefined') rememberLoginDestination(location.pathname, sessionStorage); }, []); return <div className="login-page"><main className="login-card"><div className="mark">⌖</div><h1>Карта IT‑работодателей</h1><p>Введите логин и пароль, чтобы открыть рейтинг и карту офисов.</p><form method="post" action="/auth"><label>Логин<input className="field" name="login" type="text" autoComplete="username" autoFocus required/></label><label>Пароль<input className="field" name="password" type="password" autoComplete="current-password" required/></label>{error && <div className="error" role="alert">Логин или пароль не подошли.</div>}<button type="submit">Открыть карту</button></form><div className="foot">Доступ сохранится на этом устройстве на 30 дней</div></main></div>; }
function Workspace({ user }: {
    user: Session;
}) { const [settingsTab, setSettingsTab] = useSectionState('settings:tab', 'general'); const [listType,setListType]=useState<EntityType>('company'),[vacanciesType,setVacanciesType]=useState<EntityType>('company'); const [section, setSection] = useState<string>(() => { const initial = typeof sessionStorage === 'undefined' ? sectionFromPath(location.pathname) : restoreLoginDestination(location.pathname, sessionStorage); if (typeof history !== 'undefined' && ['http:', 'https:'].includes(location.protocol) && initial !== sectionFromPath(location.pathname)) history.replaceState(null, '', sectionPath(initial)); return initial; }), [filters, setFilters] = useFilters(), [vf, setVf] = useState(defaultVacancyFilters), [query, setQuery] = useState(''), [vacancyQuery, setVacancyQuery] = useState(''), [count, setCount] = useState(0), [company, setCompany] = useState<{
    name: string;
    type?: EntityType;
} | null>(null), [vacancy, setVacancy] = useState<{ id: number; view: EntityType } | null>(null), [channel, setChannel] = useState<number | null>(null), [editor, setEditor] = useState<{
    owner: {
        name: string;
        entityType: EntityType;
    };
    record?: VacancyDetail;
    view?: EntityType;
} | null>(null), [standalone, setStandalone] = useState(false); const drawerEntries = useRef(new Map<number, {section: string; company: {name:string; type?:EntityType} | null; vacancy: {id:number;view:EntityType} | null; channel: number | null}>());
 const drawerSequence = useRef(0);
 const approvedPop = useRef(false);
 const currentView = useRef({section, company, vacancy, channel}); currentView.current = {section, company, vacancy, channel};
 const drawerFocusOrigin = useRef<HTMLElement | null>(null);
 const rememberDrawerFocus = () => {
   if (typeof document === 'undefined') return;
   const el = document.activeElement as HTMLElement | null;
   if (el && typeof el.focus === 'function' && !el.closest?.('[role="dialog"]')) drawerFocusOrigin.current = el;
 };
 const saveDrawerHistory = (next: {company?: {name:string;type?:EntityType} | null; vacancy?: {id:number;view:EntityType} | null; channel?:number|null}) => {
   if (typeof history === 'undefined' || !['http:', 'https:'].includes(location.protocol)) return;
   const token = ++drawerSequence.current;
   drawerEntries.current.set(token, {...currentView.current, ...next});
   history.pushState({itmapDrawerToken: token}, '', sectionPath(currentView.current.section as Section));
 };
 const nav = useMemo<Navigation>(() => ({ vacancyDeleted: id => { setVacancy(current => current?.id === id ? null : current); }, openCompany: (name, type) => { if (confirmNavigation()) {
        rememberDrawerFocus();
        saveDrawerHistory({company:{name,type},vacancy:null,channel:null});
        setCompany({ name, type });
        setVacancy(null);
        setChannel(null);
        return true;
    } return false; }, openVacancy: (id, view = 'company') => { if (confirmNavigation()) {
        rememberDrawerFocus();
        saveDrawerHistory({vacancy:{id,view},channel:null});
        setVacancy({ id, view });
        setChannel(null);
    } }, openChannel: id => { if (confirmNavigation()) {
        rememberDrawerFocus();
        saveDrawerHistory({channel:id,vacancy:null});
        setChannel(id);
        setVacancy(null);
    } } }), []); useEffect(() => { const mode = window.matchMedia('(display-mode: standalone)'); const update = () => setStandalone(mode.matches || ('standalone' in navigator && !!navigator.standalone)); update(); mode.addEventListener('change', update); if ('serviceWorker' in navigator)
    void navigator.serviceWorker.register('/service-worker.js').catch(() => { }); return () => mode.removeEventListener('change', update); }, []); function change(next:string,type:EntityType='company') {
 const changed=next!==section||(next==='list'&&type!==listType)||(next==='vacancies'&&type!==vacanciesType)||company||vacancy||channel||editor;
 if(changed&&!confirmNavigation())return false;
 if (next !== section && typeof history !== 'undefined' && ['http:', 'https:'].includes(location.protocol)) history.pushState(null, '', sectionPath(next as Section));
 if(next==='list')setListType(type);if(next==='vacancies')setVacanciesType(type);
 setSection(next);setCompany(null);setVacancy(null);setChannel(null);setEditor(null);return true;
 } useEffect(() => {
    const onPop = () => {
        if (!approvedPop.current && !confirmNavigation()) { if (typeof history !== 'undefined' && ['http:', 'https:'].includes(location.protocol)) history.pushState(null, '', sectionPath(section as Section)); return; }
        approvedPop.current = false;
        const closingDrawer = !!(currentView.current.company || currentView.current.vacancy || currentView.current.channel);
        const entry = typeof history === 'undefined' ? undefined : drawerEntries.current.get(history.state?.itmapDrawerToken);
        setSection(entry?.section ?? sectionFromPath(location.pathname));
        setCompany(entry?.company ?? null); setVacancy(entry?.vacancy ?? null); setChannel(entry?.channel ?? null); setEditor(null);
        // Focus is restored to the exact opener when history closes a drawer.
        // On a section change without drawers, focus the active navigation item.
        if (!entry?.company && !entry?.vacancy && !entry?.channel)
            requestAnimationFrame(() => {
                const origin = drawerFocusOrigin.current;
                if (closingDrawer && origin?.isConnected && typeof origin.focus === 'function')
                    origin.focus({preventScroll: true});
                else document.querySelector<HTMLButtonElement>('.app-nav-button[aria-current="page"]')?.focus({preventScroll: true});
            });
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
 }, [section]);
 function closeDrawer(fallback: () => void) {
   if (!confirmNavigation()) return;
   if (typeof history !== 'undefined' && ['http:', 'https:'].includes(location.protocol) && drawerEntries.current.has(history.state?.itmapDrawerToken)) { approvedPop.current = true; history.back(); }
   else fallback();
 }
 const showTools = ['map', 'list', 'vacancies'].includes(section); return <div className={'app' + (company ? ' panel-open' : '') + (standalone ? ' pwa-standalone' : '')} data-section={section} data-admin={user.isAdmin}><header>{showTools && <div className="map-tools"><SectionSearch section={section} value={section === 'vacancies' ? vacancyQuery : query} onChange={section === 'vacancies' ? setVacancyQuery : setQuery}/><RankFilter value={filters} onChange={setFilters} count={count}/>{section === 'vacancies' && <><FilterMenu prefix="vacancy-level" title="Уровень" label={'Уровень · '+(vf.levels.length||'Все')}>{levels.map(value=><label className="vacancy-level-option" key={value}><input type="checkbox" checked={vf.levels.includes(value)} onChange={e=>setVf({...vf,levels:e.target.checked?[...vf.levels,value]:vf.levels.filter(item=>item!==value)})}/><span>{value}</span></label>)}</FilterMenu><FilterMenu prefix="vacancy-role" title="Роль" label={'Роль · '+(vf.roleIds.length||'Все')}><RoleTreeOptions selected={vf.roleIds} onChange={roleIds=>setVf({...vf,roleIds})}/></FilterMenu><FilterMenu prefix="vacancy-state" title="Признаки вакансии" label="Признаки"><label className="vacancy-state-option"><input type="checkbox" checked={vf.favoriteOnly} onChange={e => setVf({ ...vf, favoriteOnly: e.target.checked })}/>Только избранные</label><label className="vacancy-state-option"><input type="checkbox" checked={vf.showHidden} onChange={e => setVf({ ...vf, showHidden: e.target.checked })}/>Показать скрытые</label></FilterMenu></>}</div>}<nav className="app-nav" aria-label="Основные разделы">{sections.map(([key, label, icon]) => key==='list'||key==='vacancies'?<EntityNavigation key={key} section={key} label={label} icon={icon} active={section===key} type={key==='list'?listType:vacanciesType} onSelect={type=>change(key,type)}/>:key === 'settings' ? <SettingsNavigation key={key} admin={user.isAdmin} active={section === 'settings'} tab={settingsTab} onSelect={next => { if ((section !== 'settings' || settingsTab !== next || company || vacancy || channel || editor) && !confirmNavigation()) return false; setSettingsTab(next); if (section !== 'settings' && typeof history !== 'undefined' && ['http:', 'https:'].includes(location.protocol)) history.pushState(null, '', sectionPath('settings')); setSection('settings'); setCompany(null); setVacancy(null); setChannel(null); setEditor(null); return true; }}/> : <button key={key} type="button" className="app-nav-button" aria-current={section === key ? 'page' : undefined} onClick={() => change(key)}><span className="app-nav-icon"><Icon name={icon}/></span><span>{label}</span></button>)}</nav></header><main className="main"><section className={'app-section active ' + (section === 'list' ? 'company-list' : section) + '-section'} aria-label={sections.find(([key]) => key === section)?.[1]}><Suspense fallback={<div className="empty">Загрузка…</div>}>{section === 'tasks' ? <Tasks nav={nav}/> : section === 'applications' ? <Applications nav={nav}/> : section === 'vacancies' ? <Vacancies type={vacanciesType} nav={nav} admin={user.isAdmin} filters={filters} vacancyFilters={vf} query={vacancyQuery} onCount={setCount}/> : section === 'list' ? <CompanyList key={listType} type={listType} nav={nav} admin={user.isAdmin} filters={filters} query={query} onCount={setCount}/> : section === 'map' ? <MapSection nav={nav} filters={filters} query={query} onQueryChange={setQuery} onCount={setCount} selectedCompany={company?.type === 'agency' ? undefined : company?.name}/> : <Settings user={user} tab={settingsTab} onTabChange={next=>{if(confirmNavigation())setSettingsTab(next);}}/>}</Suspense></section>{company && <PanelBoundary kind="company" title={company.name} onClose={() => closeDrawer(() => setCompany(null))}><CompanyDrawer key={company.name} name={company.name} type={company.type} nav={nav} admin={user.isAdmin} onClose={() => closeDrawer(() => setCompany(null))} onCreate={c => setEditor({ owner: { name: c.name, entityType: c.entityType } })} onEdit={(v, view) => { if (confirmNavigation()) { setVacancy(null); setEditor({ owner: { name: v.companyName, entityType: v.entityType }, record: v, view }); } }}/></PanelBoundary>} {vacancy && <PanelBoundary kind="vacancy" title="Загрузка вакансии…" onClose={() => closeDrawer(() => setVacancy(null))}><VacancyDrawer key={vacancy.id + ":" + vacancy.view} id={vacancy.id} view={vacancy.view} nav={nav} admin={user.isAdmin} onClose={() => closeDrawer(() => setVacancy(null))} onEdit={v => { setEditor({ owner: { name: v.companyName, entityType: v.entityType }, record: v, view: vacancy.view }); setVacancy(null); }}/></PanelBoundary>}{channel && <PanelBoundary kind="channel" title="Telegram-канал" onClose={() => closeDrawer(() => setChannel(null))}><ChannelDrawer key={channel} id={channel} nav={nav} admin={user.isAdmin} onClose={() => closeDrawer(() => setChannel(null))}/></PanelBoundary>} {editor && user.isAdmin && <PanelBoundary kind="editor" title={editor.record ? "Редактирование вакансии" : "Новая вакансия"} onClose={() => setEditor(null)}><VacancyEditor key={editor.record?.id || editor.owner.name} {...editor} onClose={() => setEditor(null)} onSaved={id => { setEditor(null); if (id)
    setVacancy({ id, view: editor.view || editor.owner.entityType }); }}/></PanelBoundary>}</main></div>; }
export default function Application() { const q = useApi<Session>('/api/session', {refreshIntervalMs: 60_000}); useEffect(() => { const expire = () => { allowSessionExit(); clearCache(); location.replace('/logout?expired=1'); }; window.addEventListener('session-expired', expire); return () => window.removeEventListener('session-expired', expire); }, []); if (q.error instanceof ApiError && q.error.status === 401)
    return <Login />; return <State loading={!q.data && q.loading} error={q.error}>{q.data && <Workspace user={q.data}/>}</State>; }
