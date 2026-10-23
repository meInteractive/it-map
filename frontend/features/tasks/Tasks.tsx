import { useState } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, ErrorText, Field, Heading, Icon, EntitySwitch, State, moscowParts, dateLabel } from '../../components/Common';
import type { Task, TaskList, EntityType, Navigation } from '../../../shared/contracts';
const taskTypes: Record<string, [
    string,
    string
]> = { call: ['Звонок', 'phone'], email: ['Письмо', 'mail'], messenger: ['Сообщение', 'message'], meeting: ['Встреча', 'users'] };
export interface CalendarStatus {
    configured: boolean;
    connected: boolean;
    accountEmail?: string;
    syncedTasks?: number;
    syncErrors?: number;
    message?: string;
}
export function CalendarCard() { const q = useApi<CalendarStatus>('/api/google-calendar/status'); const [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false); const d = q.data; async function action(name: string) { if (name === 'disconnect' && !confirm('Отключить Google Calendar?'))
    return; setBusy(true); setError(undefined); try {
    await mutate('/api/google-calendar/' + name, 'POST');
}
catch (e) {
    setError(e);
}
finally {
    setBusy(false);
} } return <article className="google-calendar-card"><div className="google-calendar-mark" aria-hidden="true">31</div><div className="google-calendar-copy"><div className="google-calendar-title"><span className="google-calendar-dot"/>Google Calendar</div><div className="google-calendar-status">{q.loading ? 'Проверяем подключение…' : d?.connected ? 'Календарь подключён' : d?.configured ? 'Календарь не подключён' : 'Подключение пока не настроено'}</div><div className="google-calendar-account">{d?.accountEmail}{d?.syncErrors ? ` · Ошибок синхронизации: ${d.syncErrors}` : ''}</div><ErrorText error={error || q.error}/></div><div className="google-calendar-actions">{d?.connected ? <><button className="google-calendar-button secondary" disabled={busy} onClick={() => void action('sync')}>Синхронизировать задачи</button><button className="google-calendar-button danger" disabled={busy} onClick={() => void action('disconnect')}>Отключить</button></> : <button className="google-calendar-button" disabled={!d?.configured || busy} onClick={() => location.assign('/api/google-calendar/connect')}>Подключить календарь</button>}</div></article>; }
function initialSchedule(value: Date) { const p = moscowParts(value); return { date: `${p.year}-${p.month}-${p.day}`, time: `${p.hour}:${p.minute}` }; }
export function scheduleIso(date: string, time: string) { return new Date(date + 'T' + time + ':00+03:00').toISOString(); }
export function TaskCard({ task, nav, includeCompany = true }: {
    task: Task;
    nav: Navigation;
    includeCompany?: boolean;
}) { const [editing, setEditing] = useState(false), [schedule, setSchedule] = useState(() => initialSchedule(new Date(task.dueAt))), [error, setError] = useState<unknown>(), [busy, setBusy] = useState(false); const meta = taskTypes[task.type] || [task.type, 'checklist']; const urgency = task.completed ? ' completed' : new Date(task.dueAt).getTime() < Date.now() ? ' overdue' : initialSchedule(new Date(task.dueAt)).date === initialSchedule(new Date()).date ? ' today' : ''; return <article className={'task-card' + urgency}><div className="task-icon"><Icon name={meta[1]}/></div><div className="task-main"><div className="task-title"><b>{meta[0]}</b>{includeCompany && <button className="task-company" onClick={() => nav.openCompany(task.companyName)}>{task.companyName}</button>}{task.vacancyId && <button className="task-vacancy" onClick={() => nav.openVacancy(task.vacancyId!)}>{task.vacancyTitle || 'Открыть вакансию'}</button>}{task.googleSyncStatus === 'error' ? <span className="task-calendar-badge error" title={task.googleSyncError}>Ошибка календаря</span> : task.googleEventId ? <span className="task-calendar-badge">Google Calendar</span> : null}</div>{task.note && <div className="task-note">{task.note}</div>}<label className="task-complete"><input type="checkbox" checked={task.completed} disabled={busy} onChange={async (e) => { setBusy(true); setError(undefined); try {
    await mutate('/api/tasks/' + task.id, 'PUT', { completed: e.target.checked });
}
catch (err) {
    setError(err);
}
finally {
    setBusy(false);
} }}/> Выполнено</label></div><time className="task-due" dateTime={task.dueAt}>{dateLabel(task.dueAt)}</time><div className="task-schedule"><button className="task-edit-button" aria-expanded={editing} onClick={() => setEditing(!editing)}>Изменить дату и время</button>{editing && <ActionForm className="task-edit-form" onSubmit={async () => { await mutate('/api/tasks/' + task.id, 'PUT', { dueAt: scheduleIso(schedule.date, schedule.time) }); setEditing(false); }}>{saving => <><div className="task-form-grid"><Field label="Дата, МСК"><input className="task-input" type="date" required value={schedule.date} onChange={e => setSchedule({ ...schedule, date: e.target.value })}/></Field><Field label="Время, МСК"><input className="task-input" type="time" required value={schedule.time} onChange={e => setSchedule({ ...schedule, time: e.target.value })}/></Field></div><div className="task-form-actions"><button type="submit" className="task-add" disabled={saving}>Сохранить</button><button type="button" className="task-edit-button" onClick={() => setEditing(false)}>Отмена</button></div></>}</ActionForm>}<ErrorText error={error}/></div></article>; }
function TaskForm({ companyName, vacancyId }: {
    companyName: string;
    vacancyId?: number;
}) { const [type, setType] = useState('call'), [note, setNote] = useState(''), [schedule, setSchedule] = useState(() => initialSchedule(new Date(Date.now() + 3600000))); return <ActionForm className="company-task-form" onSubmit={async () => { await mutate('/api/tasks', 'POST', { companyName, vacancyId: vacancyId || null, type, note, dueAt: scheduleIso(schedule.date, schedule.time) }); setNote(''); }}>{busy => <><div className="task-form-grid"><Field label="Тип задачи"><select className="task-input" value={type} onChange={e => setType(e.target.value)}>{Object.entries(taskTypes).map(([k, [v]]) => <option key={k} value={k}>{v}</option>)}</select></Field><Field label="Дата, МСК"><input className="task-input" type="date" required value={schedule.date} onChange={e => setSchedule({ ...schedule, date: e.target.value })}/></Field><Field label="Время, МСК"><input className="task-input" type="time" required value={schedule.time} onChange={e => setSchedule({ ...schedule, time: e.target.value })}/></Field><Field label="Заметка"><textarea className="task-input" maxLength={1200} value={note} onChange={e => setNote(e.target.value)}/></Field></div><div className="task-form-actions"><button type="submit" className="task-add" disabled={busy}>Добавить задачу</button></div></>}</ActionForm>; }
export default function Tasks({ nav, companyName, vacancyId }: {
    nav: Navigation;
    companyName?: string;
    vacancyId?: number;
}) { const [completed, setCompleted] = useState(false), [type, setType] = useState<EntityType>('company'); const params = new URLSearchParams(); if (companyName)
    params.set('company', companyName); if (vacancyId)
    params.set('vacancy', String(vacancyId)); if (completed)
    params.set('includeCompleted', '1'); const q = useApi<TaskList>('/api/tasks' + (params.size ? '?' + params : '')); const entities = new Map(q.data?.entities.map(e => [e.name, e])); const tasks = (q.data?.tasks || []).filter(t => companyName || (entities.get(t.companyName)?.entityType || 'company') === type); return <div className={companyName ? 'company-task-list' : 'tasks-shell'}>{!companyName ? <><Heading title="Задачи" subtitle="Ближайшие действия, время московское"><EntitySwitch value={type} onChange={setType}/><label className="show-completed"><input type="checkbox" checked={completed} onChange={e => setCompleted(e.target.checked)}/>Показать завершённые</label><span className="applications-count">{tasks.length}</span></Heading><CalendarCard /></> : <><TaskForm companyName={companyName} vacancyId={vacancyId}/><label className="show-completed"><input type="checkbox" checked={completed} onChange={e => setCompleted(e.target.checked)}/>Показать завершённые</label></>}<State loading={!q.data && q.loading} error={q.error}><div className="task-list">{tasks.length ? tasks.map(t => <TaskCard key={t.id} task={t} nav={nav} includeCompany={!companyName}/>) : <div className="task-empty">Задач пока нет.</div>}</div></State></div>; }
