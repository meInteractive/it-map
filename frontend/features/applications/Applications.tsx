import {useSectionState} from '../../hooks/useSectionState';
import { useCallback, useEffect, useRef, useState } from 'react';
import {ApplicationConflict, isApplicationConflict} from './ApplicationConflict';
import {useCreationIntent} from './useCreationIntent';
import { mutate, useApi } from '../../api/client';
import { ActionForm, EntitySwitch, ErrorText, Heading, Icon, State, dateLabel, rank } from '../../components/Common';
import { Markdown, MarkdownEditor } from '../../components/Markdown';
import { statuses, sourceLabels, type ApplicationRecord, type ApplicationsResponse, type EntityType, type Navigation, type Vacancy } from '../../../shared/contracts';
export function applicationMatchesStatusFilter(record: Pick<ApplicationRecord, 'status'>, filter: string) { return filter === '__none__' ? !record.status : filter ? record.status === filter : record.status !== 'Отказ'; }
export function StatusOptions() { return <>{statuses.map(s => <option key={s} value={s}>{s || 'Нет отклика'}</option>)}</>; }
export function SourcesEditor({ value, onChange }: {
    value: string[];
    onChange: (v: string[]) => void;
}) { return <fieldset className="crm-sources"><legend>Где откликнулись</legend><div className="crm-source-options">{Object.entries(sourceLabels).map(([key, label]) => <label className="crm-source-option" key={key}><input type="checkbox" checked={value.includes(key)} onChange={e => onChange(e.target.checked ? [...value, key] : value.filter(v => v !== key))}/>{label}</label>)}</div></fieldset>; }
const blank = { status: 'Откликнулся', comment: '', recruiterName: '', recruiterPhone: '', recruiterEmail: '', recruiterContactType: '', applicationSources: [] as string[], vacancyId: null as number | null };
export function ApplicationForm({ companyName, vacancyId, record, vacancies = [], onSaved, onBusyChange, disabled = false }: {
    companyName: string;
    vacancyId?: number;
    record?: ApplicationRecord;
    vacancies?: Vacancy[];
    onSaved?: () => void;
    onBusyChange?: (busy: boolean) => void;
    disabled?: boolean;
}) {
    const [base, setBase] = useState(record);
    const [target] = useState({companyName, vacancyId});
    const creation = useCreationIntent(!base);
    const [value, setValue] = useState({ ...blank, ...record, ...(vacancyId ? { vacancyId } : {}) });
    const [conflicted, setConflicted] = useState(false);
    const [formEpoch, setFormEpoch] = useState(0);
    const set = (key: string, v: string | string[] | number | null) => setValue({ ...value, [key]: v });
    return <ActionForm key={formEpoch} lockWhileBusy disabled={disabled} onBusyChange={onBusyChange} className={'crm-form' + (target.vacancyId ? ' vacancy-application-form' : '')} onSubmit={async () => {
        try {
            const response = await mutate<ApplicationsResponse & {createdApplicationId?: number}>(base ? '/api/applications/' + base.id : '/api/applications/create', base ? 'PUT' : 'POST', {
                ...value, companyName: target.companyName, ...(!base ? creation.value() : {}),
            });
            const savedId = base?.id ?? response.createdApplicationId;
            const saved = savedId == null ? undefined : response.records?.find(r => r.id === savedId);
            // Only our successful ACK may advance a frozen draft. A background
            // GET or refreshed props must not attach a new token to old input.
            if (saved) { setBase(saved); setValue({...blank, ...saved}); }
            onSaved?.();
        } catch (error) { setConflicted(!!base && isApplicationConflict(error)); throw error; }
    }}>{busy => <>
        {target.vacancyId ? !base && <div className="vacancy-application-intro">Отклик по этой вакансии ещё не создан. Заполните данные и нажмите «Сохранить», чтобы связать отклик с вакансией.</div> : <h3 className="crm-form-title">{base ? 'Редактировать отклик' : 'Новый отклик'}</h3>}
        {!target.vacancyId && <label className="crm-field"><span>Вакансия</span><select className="crm-select" value={value.vacancyId || ''} onChange={e => set('vacancyId', e.target.value ? Number(e.target.value) : null)}><option value="">Без вакансии</option>{vacancies.map(v => <option key={v.id} value={v.id}>{v.title}</option>)}</select></label>}
        <label className="crm-field"><span>Статус отклика</span><select className="crm-select" aria-label="Статус отклика" value={value.status} onChange={e => set('status', e.target.value)}><StatusOptions /></select></label>
        <div className="crm-field"><SourcesEditor value={value.applicationSources} onChange={v => set('applicationSources', v)}/></div>
        <div className="crm-field"><p className="crm-contact-heading">Контакт рекрутера</p><RecruiterEditor value={value} onChange={(k, v) => set(k, v)}/></div>
        <div className="crm-field"><span>Комментарий</span><MarkdownEditor value={value.comment} onChange={v => set('comment', v)} limit={1000}
            placeholder={target.vacancyId ? 'Впечатления от разговора, что уточнить на следующем этапе…' : 'Например: впечатления от разговора, что уточнить на следующем этапе…'}/></div>
        {conflicted && base && <ApplicationConflict base={base} draft={value} vacancies={vacancies} onResolved={(current, merged) => {
            setBase(current); setValue(merged); setConflicted(false); setFormEpoch(epoch => epoch + 1);
        }}/>}
        {!base && <>
            {creation.loading && <p className="muted" role="status">Подготовка создания отклика…</p>}
            <ErrorText error={creation.error}/>
            {!!creation.error && <button type="button" disabled={busy || creation.loading} onClick={creation.retry}>Повторить подготовку</button>}
        </>}
        <div className={target.vacancyId ? 'vacancy-application-actions' : 'crm-actions'}><button type="submit" className={target.vacancyId ? 'vacancy-application-save' : 'crm-save'} disabled={busy || conflicted || (!base && !creation.ready)}>Сохранить</button></div>
    </>}</ActionForm>;
}
function RecruiterEditor({ value, onChange }: {
    value: typeof blank;
    onChange: (key: string, v: string) => void;
}) {
    return <div className="crm-contact-grid">
        <label><span>Имя рекрутера</span><input type="text" maxLength={160} autoComplete="name"
            placeholder="Например, Анна Иванова" value={value.recruiterName}
            onChange={e => onChange('recruiterName', e.target.value)}/></label>
        <label><span>Телефон</span><input type="tel" maxLength={64} autoComplete="tel"
            placeholder="+7 999 123-45-67" value={value.recruiterPhone}
            onChange={e => onChange('recruiterPhone', e.target.value)}/></label>
        <label><span>Email</span><input type="email" maxLength={254} autoComplete="email"
            placeholder="anna@company.ru" value={value.recruiterEmail}
            onChange={e => onChange('recruiterEmail', e.target.value)}/></label>
        <label><span>Тип контакта</span><select value={value.recruiterContactType}
            onChange={e => onChange('recruiterContactType', e.target.value)}>
            <option value="">Не указан</option><option value="direct">Прямой работодатель</option>
            <option value="agency">Агентство</option>
        </select></label>
    </div>;
}
export function History({ record }: {
    record: Pick<ApplicationRecord, 'history'>;
}) {
    if (!record.history?.length) return <span className="meta">История пока пустая</span>;
    return <div className="application-history-list">{record.history.map((h, i) => {
        const from = h.fromStatus || 'Нет отклика', to = h.toStatus || 'Нет отклика';
        return <div key={i} className="application-history-item"><b>{to}</b>
            <span>{dateLabel(h.changedAt)} · {from} → {to}</span>
        </div>;
    })}</div>;
}
export function DeleteApplication({ record, onDeleted, disabled = false, className = 'application-delete', onBusyChange }: {
    record: ApplicationRecord;
    onDeleted?: () => void;
    disabled?: boolean;
    className?: string;
    onBusyChange?: (busy: boolean) => void;
}) { const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(); const inFlight = useRef(false); return <><button type="button" className={className} title="Удалить отклик" aria-label={'Удалить отклик ' + (record.vacancyTitle || 'без вакансии')} disabled={busy || disabled} onClick={async () => { if (inFlight.current || disabled) return; if (!confirm('Удалить этот отклик и историю его статусов?'))
    return; inFlight.current = true; setBusy(true); setError(undefined); try {
    onBusyChange?.(true);
    await mutate('/api/applications/' + record.id, 'DELETE');
    onDeleted?.();
}
catch (e) {
    setError(e);
}
finally {
    inFlight.current = false;
    setBusy(false);
    onBusyChange?.(false);
} }}><Icon name="trash"/></button><ErrorText error={error}/></>; }
type InlineEditor = {kind: 'comment' | 'contact' | 'status'; base: ApplicationRecord; draft: ApplicationRecord};
function ApplicationRow({ record, nav, companyRank, onEditingChange }: {
    record: ApplicationRecord;
    nav: Navigation;
    companyRank?: number | null;
    onEditingChange: (id: number, base: ApplicationRecord | null) => void;
}) {
    const [editor, setEditor] = useState<InlineEditor | null>(null);
    const [conflicted, setConflicted] = useState(false), [error, setError] = useState<unknown>();
    const [formBusy, setFormBusy] = useState(false);
    const [formEpoch, setFormEpoch] = useState(0), [statusBusy, setStatusBusy] = useState(false);
    function start(kind: InlineEditor['kind'], draft = record) {
        setEditor({kind, base: record, draft}); setConflicted(false); setError(undefined);
        onEditingChange(record.id, record);
    }
    function update(values: Partial<ApplicationRecord>) {
        setEditor(previous => previous && {...previous, draft: {...previous.draft, ...values}});
    }
    function close() { setEditor(null); setConflicted(false); setError(undefined); onEditingChange(record.id, null); }
    async function save(draft = editor?.draft) {
        if (!draft) return;
        try { await mutate('/api/applications/' + record.id, 'PUT', draft); close(); }
        catch (err) { setConflicted(isApplicationConflict(err)); throw err; }
    }
    const recovery = editor && conflicted && <ApplicationConflict base={editor.base} draft={editor.draft} onResolved={(current, merged) => {
        setEditor({...editor, base: current, draft: merged}); setConflicted(false); setError(undefined); setFormEpoch(epoch => epoch + 1);
    }}/>;
    const controls = (busy: boolean) => <><button type="submit" disabled={busy || statusBusy || conflicted}>Сохранить</button><button type="button" disabled={busy || statusBusy} onClick={close}>Отмена</button></>;
    return <tr>
        <td data-label="Компания"><span className="application-company-rank">№ {rank(companyRank)}</span><button className="application-company" onClick={() => nav.openCompany(record.companyName)}>{record.companyName}</button></td>
        <td data-label="Вакансия"><div className={'application-vacancy' + (!record.vacancyTitle ? ' empty' : '')}>{record.vacancyTitle || 'Без вакансии'}</div></td>
        <td data-label="Где откликнулись"><div className="application-sources">{record.applicationSources?.map(s => <span key={s} className="application-source">{sourceLabels[s] || s}</span>)}</div></td>
        <td data-label="Статус"><div className="application-status-actions">
            {editor?.kind === 'status' ? <ActionForm key={formEpoch} lockWhileBusy disabled={statusBusy} onBusyChange={setFormBusy} className="application-editor" onSubmit={() => save()}>{busy => <>
                <select className="application-status" aria-label={'Статус отклика ' + record.companyName} value={editor.draft.status} disabled={busy || statusBusy} onChange={e => update({status: e.target.value})}><StatusOptions /></select>
                {recovery}{controls(busy)}
            </>}</ActionForm> : <select className="application-status" aria-label={'Статус отклика ' + record.companyName} disabled={!!editor} value={record.status} onChange={async e => {
                const draft = {...record, status: e.target.value}; start('status', draft); setStatusBusy(true);
                try { await save(draft); } catch (err) { setError(err); } finally { setStatusBusy(false); }
            }}><StatusOptions /></select>}
            <DeleteApplication record={record} disabled={formBusy || statusBusy} onDeleted={close}/>
        </div><ErrorText error={error}/></td>
        <td data-label="Рекрутер">{editor?.kind === 'contact' ? <ActionForm key={formEpoch} lockWhileBusy onBusyChange={setFormBusy} className="application-contact-editor" onSubmit={() => save()}>{busy => <>
            <RecruiterEditor value={editor.draft} onChange={(key, value) => update({[key]: value})}/>
            {recovery}<div className="application-contact-editor-actions">{controls(busy)}</div>
        </>}</ActionForm> : <button className="application-contact application-edit-button" aria-label="Редактировать контакты рекрутера" disabled={!!editor} onClick={() => start('contact')}>
            <b className="application-contact-name">{record.recruiterName || 'Добавить контакты'}</b><span className="application-contact-type">{{ direct: 'Прямой работодатель', agency: 'Агентство' }[record.recruiterContactType] || ''}</span><span className="application-contact-links">{record.recruiterPhone}<br />{record.recruiterEmail}</span>
        </button>}</td>
        <td data-label="История"><History record={record}/></td>
        <td data-label="Комментарий">{editor?.kind === 'comment' ? <ActionForm key={formEpoch} lockWhileBusy disabled={statusBusy} onBusyChange={setFormBusy} className="application-editor" onSubmit={() => save()}>{busy => <>
            <MarkdownEditor value={editor.draft.comment} onChange={comment => update({comment})} limit={1000}/>{recovery}{controls(busy)}
        </>}</ActionForm> : <div className={'application-comment rendered-md' + (!record.comment ? ' empty' : '')} role="button" tabIndex={editor ? -1 : 0} aria-disabled={!!editor} onClick={() => {if (!editor) start('comment');}} onKeyDown={e => {if (!editor && e.key === 'Enter') start('comment');}}>
            {record.comment ? <Markdown value={record.comment}/> : <>Добавить комментарий</>}
        </div>}</td>
    </tr>;
}
export function CompanyApplications({ companyName, vacancyId }: {
    companyName: string;
    vacancyId?: number;
}) { const q = useApi<ApplicationsResponse>('/api/applications?company=' + encodeURIComponent(companyName)), [selected, setSelected] = useState<number | 'new' | null>(vacancyId ? 'new' : null); const [saving, setSaving] = useState(false), [deleting, setDeleting] = useState(false); const locked = saving || deleting; const records = (q.data?.records || []).filter(r => !vacancyId || r.vacancyId === vacancyId); const [formRecord, setFormRecord] = useState<ApplicationRecord>(); const current = records.find(r => r.id === selected) || (vacancyId ? records[0] : undefined) || (selected != null ? formRecord : undefined); useEffect(() => { setFormRecord(current); }, [current?.id]); return <><State loading={!q.data && q.loading} error={q.data ? undefined : q.error}><ErrorText error={q.data ? q.error : undefined}/><div className="company-applications-head"><h2>Отклики</h2><div className="company-applications-actions"><span className="company-applications-limit">{q.data?.records.length || 0} из 10</span><button className="company-application-new" disabled={locked || (q.data?.records.length || 0) >= 10} onClick={() => { setFormRecord(undefined); setSelected('new'); }}>Новый отклик</button></div></div><div className="company-applications-list">{records.map(r => <div key={r.id} className="company-application-row"><button className={'company-application-card' + (current?.id === r.id ? ' active' : '')} disabled={locked} onClick={() => setSelected(r.id)}><b>{r.vacancyTitle || 'Без вакансии'}</b><span>{r.recruiterName}</span><em>{r.status || 'Нет отклика'}</em></button><DeleteApplication onBusyChange={setDeleting} className="company-application-delete" record={r} disabled={locked} onDeleted={() => { if (current?.id !== r.id) return; setFormRecord(undefined); setSelected(vacancyId ? 'new' : null); }}/></div>)}</div>{!records.length && <div className="company-applications-empty">Откликов пока нет. Можно создать до 10 записей.</div>}{(current || selected === 'new') && <><ApplicationForm disabled={deleting} key={current?.id || 'new'} companyName={companyName} vacancyId={vacancyId} record={current} vacancies={q.data?.vacancies} onBusyChange={setSaving} onSaved={() => { setFormRecord(undefined); setSelected(null); }}/>{!vacancyId && current && <History record={current}/>}</>}{!!vacancyId && <div className="crm-history"><h3>История изменения отклика</h3><History record={current || {history: []}}/></div>}</State>{q.error && <button type="button" disabled={q.loading} onClick={q.reload}>Повторить загрузку откликов</button>}</>; }
export default function Applications({ nav }: {
    nav: Navigation;
}) { const q = useApi<ApplicationsResponse>('/api/crm/applications'), [type, setType] = useSectionState<EntityType>('applications:type','company'), [filter, setFilter] = useSectionState('applications:status',''); const [editing, setEditing] = useState(new Map<number, ApplicationRecord>()); const onEditingChange = useCallback((id: number, base: ApplicationRecord | null) => { setEditing(previous => { const next = new Map(previous); if (base) next.set(id, base); else next.delete(id); return next; }); }, []); const entities = new Map(q.data?.entities?.map(e => [e.name, e])); const records = (q.data?.records || []).filter(r => (entities.get(r.companyName)?.entityType || 'company') === type && applicationMatchesStatusFilter(r, filter)); for (const [id, base] of editing) { if (!records.some(record => record.id === id)) records.push(q.data?.records.find(record => record.id === id) || base); } return <div className="applications-shell"><Heading actionsClassName="applications-heading-actions" title="Отклики" subtitle="Организации, по которым уже начата работа"><EntitySwitch value={type} onChange={setType}/><label className="application-status-filter">Статус<select aria-label="Фильтр по статусу отклика" value={filter} onChange={e => setFilter(e.target.value)}><option value="">Все, кроме отказов</option><option value="__none__">Нет отклика</option>{statuses.filter(Boolean).map(s => <option key={s}>{s}</option>)}</select></label><span className="applications-count">{records.length}</span></Heading><div className="applications-card"><State loading={!q.data && q.loading} error={q.data ? undefined : q.error}><ErrorText error={q.data ? q.error : undefined}/>{records.length ? <table className="applications-table"><thead><tr>{[type === 'agency' ? 'Название агентства' : 'Название компании', 'Вакансия', 'Где откликнулись', 'Статус отклика', 'Рекрутер', 'История откликов', 'Комментарий'].map(h => <th key={h}>{h}</th>)}</tr></thead><tbody>{records.map(r => <ApplicationRow key={r.id} record={r} nav={nav} companyRank={entities.get(r.companyName)?.rank} onEditingChange={onEditingChange}/>)}</tbody></table> : <div className="applications-empty">{filter ? 'Нет откликов с выбранным статусом.' : 'Нет активных откликов. Выберите «Отказ» в фильтре, чтобы увидеть отказы.'}</div>}</State></div></div>; }
