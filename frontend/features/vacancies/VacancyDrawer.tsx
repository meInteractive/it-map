import { useState } from 'react';
import { useApi } from '../../api/client';
import { State, dateLabel, safeUrl } from '../../components/Common';
import { Drawer } from '../../components/Drawer';
import { Markdown } from '../../components/Markdown';
import type { Entity, EntityType, Navigation, VacancyDetailResponse, VacancyDetail } from '../../../shared/contracts';
import { CompanyApplications } from '../applications/Applications';
import Tasks from '../tasks/Tasks';
import { VacancyActions } from './Vacancies';
import { vacancyContext, vacancySalary, publicationLabel, vacancyDateLabel } from './model';
import CoverLetter from './CoverLetter';
import {vacancyRoleText} from './RoleTree';
export function VacancyInfo({ v, entities, view, nav }: {
    v: VacancyDetail;
    entities: Entity[];
    view: EntityType;
    nav: Navigation;
}) {
    const salary = vacancySalary(v, entities, view);
    return <div className="vacancy-info-card"><h3>Краткая информация</h3>
        <div className="vacancy-drawer-facts">
            <span className="job-fact"><span className={salary.estimated ? 'vacancy-salary-estimate' : undefined}
                title={salary.estimated ? 'Оценка по вилке работодателя для указанного уровня; зарплата в вакансии не указана.' : undefined}>{salary.value}</span></span>
            {vacancyRoleText(v) && <span className="job-fact">{vacancyRoleText(v)}</span>}
            {v.level && <span className="job-fact">{v.level}</span>}
            {v.entityType === 'agency' && <><span className="job-fact">Агентство: {v.companyName}</span>
                {v.realCompanyName && <span className="job-fact">Работодатель: {v.realCompanyName}</span>}</>}
            {v.telegramSources.map(source => <span className="job-fact" key={source.channelId}>
                Канал: <button className="vacancy-company-button" type="button" onClick={() => nav.openChannel(source.channelId)}>{source.channelName}</button>
            </span>)}
            {v.telegramSources.filter(source => source.telegramContact).map(source => <span className="job-fact" key={source.channelId}>
                Telegram-контакт: {source.telegramContact}
            </span>)}
            <span className="job-fact">Релевантность: {v.relevance == null ? '—' : v.relevance + '%'}</span>
            <span className="job-fact">Опубликована: {publicationLabel(v)}</span>
            {v.loadedAt && <span className="job-fact">Загружена: {vacancyDateLabel(v.loadedAt)}</span>}
            <span className="job-fact">Создана: {dateLabel(v.createdAt)}</span>
            <span className="job-fact">Проверена: {dateLabel(v.lastCheckedAt)}</span>
            {v.archived && <span className="vacancy-archive-badge">Архивная</span>}
            <span className={'vacancy-status-badge' + (v.applicationId ? ' active' : '')}>{v.applicationStatus || 'Нет отклика'}</span>
        </div>
    </div>;
}
export default function VacancyDrawer({ id, view = 'company', nav, admin, onClose, onEdit }: {
    id: number;
    view?: EntityType;
    nav: Navigation;
    admin: boolean;
    onClose: () => void;
    onEdit: (v: VacancyDetail) => void;
}) { const q = useApi<VacancyDetailResponse>('/api/vacancies/' + id), [tab, setTab] = useState('description'); const v = q.data?.vacancy, context = v && vacancyContext(v, view), salary = v && vacancySalary(v, q.data?.entities || [], view); return <Drawer title={v?.title || 'Загрузка вакансии…'} subtitle={context && <button className="vacancy-company-button" onClick={() => context.channelId ? nav.openChannel(context.channelId) : nav.openCompany(context.name, context.type)}>{context.name}</button>} onClose={onClose}>{v && <div className="vacancy-drawer-tabs" role="tablist">{Object.entries({ description: 'Описание', letter: 'Письмо', application: 'Отклик', tasks: 'Задачи', info: 'Инфо' }).map(([k, label]) => <button key={k} className={'vacancy-drawer-tab' + (tab === k ? ' active' : '')} role="tab" aria-selected={tab === k} onClick={() => setTab(k)}>{label}</button>)}</div>}<div className="vacancy-drawer-body"><State loading={!v && q.loading} error={q.error}>{v && <>{tab === 'description' ? <><div className="vacancy-drawer-actions vacancy-drawer-primary-actions"><VacancyActions v={v} entities={q.data?.entities || []} admin={admin} nav={nav} flags/>{admin && <button className="vacancy-source-button" onClick={() => onEdit(v)}>Редактировать</button>}{safeUrl(v.url) && <a className="vacancy-source-button" href={safeUrl(v.url)} target="_blank" rel="noreferrer">Открыть источник</a>}{v.publicationSources?.filter(s=>s.url!==v.url&&safeUrl(s.url)).map(s=><a key={s.candidateId} className="vacancy-source-button" href={safeUrl(s.url)} target="_blank" rel="noreferrer">Дополнительный источник #{s.candidateId}</a>)}{v.telegramSources.map(s => <a key={s.channelId} className="vacancy-source-button" href={safeUrl(s.messageUrl)} target="_blank" rel="noreferrer">Пост в {s.channelName}</a>)}</div><div className="job-facts"><span className={salary?.estimated ? 'vacancy-salary-estimate' : undefined}>{salary?.value}</span>{[vacancyRoleText(v), v.level, 'Релевантность: ' + (v.relevance ?? '—'), v.archived ? 'Архив' : ''].filter(Boolean).map((s, i) => <span className="job-fact" key={i}>{s}</span>)}</div><Markdown className="vacancy-drawer-md markdown-content" value={v.descriptionMd} empty="Полный текст пока не добавлен."/></> : tab === 'letter' ? <CoverLetter v={v} ready={!q.loading && !q.error}/> : tab === 'application' ? <div className="vacancy-application-form"><CompanyApplications companyName={v.companyName} vacancyId={v.id}/></div> : tab === 'tasks' ? <Tasks companyName={v.companyName} vacancyId={v.id} nav={nav}/> : <VacancyInfo v={v} entities={q.data?.entities || []} view={view} nav={nav}/>}</>}</State></div></Drawer>; }
