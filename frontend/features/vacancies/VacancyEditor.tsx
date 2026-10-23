import { useState } from 'react';
import { ApiError, mutate, useApi } from '../../api/client';
import { ActionForm, Field } from '../../components/Common';
import { Drawer } from '../../components/Drawer';
import { MarkdownEditor } from '../../components/Markdown';
import { useDirty } from '../../hooks/useDirty';
import { VacancyConflict } from './VacancyConflict';
import { levels, type Channel, type EntityType, type VacancyDetail } from '../../../shared/contracts';
import {VacancyRolePicker} from './RoleTree';
interface Props {
    owner: {
        name: string;
        entityType: EntityType;
    };
    record?: VacancyDetail;
    onClose: () => void;
    onSaved: (id?: number) => void;
}
export default function VacancyEditor({ owner: ownerProp, record: recordProp, onClose, onSaved }: Props) { const [owner] = useState(ownerProp), [record] = useState(recordProp); const q = useApi<{
    channels: Channel[];
}>('/api/channels'); const source = record?.telegramSources[0]; const initial = { title: record?.title || '', url: record?.url || '', roleIds: record?.roles?.map(role=>role.id)||[], level: record?.level || '', realCompanyName: record?.realCompanyName || '', salaryFrom: record?.salaryFrom?.toString() || '', salaryTo: record?.salaryTo?.toString() || '', relevance: record?.relevance?.toString() || '', publishedAt: record?.publishedAt?.slice(0, 10) || '', loadedAt: record?.loadedAt?.slice(0, 10) || new Date().toLocaleDateString('en-CA', { timeZone: 'Europe/Moscow' }), descriptionMd: record?.descriptionMd || '', coverLetterSubject: record?.coverLetterSubject || '', coverLetterMd: record?.coverLetterMd || '', telegramChannelId: source?.channelId?.toString() || '', telegramMessageUrl: source?.messageUrl || '', telegramContact: source?.telegramContact || '' }; const [initialText] = useState(() => JSON.stringify(initial)), [value, setValue] = useState(initial), [saving, setSaving] = useState(false), [conflict, setConflict] = useState(Boolean(record && !record.revision)); const dirty = JSON.stringify(value) !== initialText; useDirty(dirty); function close() { if (!saving && (!dirty || confirm('Закрыть форму без сохранения изменений?')))
    onClose(); } function set(k: keyof typeof value, v: string) { setValue({ ...value, [k]: v }); } const channels = new Map((record?.telegramSources || []).map(s => [s.channelId, { id: s.channelId, name: s.channelName }])); q.data?.channels.forEach(c => channels.set(c.id, c)); return <Drawer title={record ? 'Редактирование вакансии' : 'Новая вакансия'} subtitle={owner.name} onClose={close} className="vacancy-drawer vacancy-editor"><div className="vacancy-drawer-body"><ActionForm className="vacancy-editor-form react-form" onSubmit={async () => { if (!value.descriptionMd.trim())
    throw new Error('Укажите полный текст вакансии.'); if (value.descriptionMd.length > 100000 || value.coverLetterMd.length > 2000)
    throw new Error('Превышен размер текста.'); if (value.salaryFrom && value.salaryTo && Number(value.salaryFrom) > Number(value.salaryTo))
    throw new Error('Нижняя граница зарплаты не может быть выше верхней.'); if (!value.roleIds.length && !record?.role) throw new Error('Укажите хотя бы одну роль вакансии.'); if (record && !record.revision) throw new Error('Обновите карточку вакансии перед редактированием.'); const payload: Record<string, unknown> = { ...value, ...(record ? {revision: record.revision} : {}), companyName: owner.name, entityType: owner.entityType, salaryFrom: value.salaryFrom ? Number(value.salaryFrom) : null, salaryTo: value.salaryTo ? Number(value.salaryTo) : null, relevance: value.relevance ? Number(value.relevance) : null };
    // Old cached detail responses can contain only the former string field.
    if (!value.roleIds.length && record?.role) { delete payload.roleIds; payload.role=record.role; }
    // Detail can contain a personal generated letter. An unrelated edit must not
    // copy that effective text into the vacancy's shared letter fields.
    if (record && value.coverLetterSubject === initial.coverLetterSubject && value.coverLetterMd === initial.coverLetterMd) {
        delete payload.coverLetterSubject;
        delete payload.coverLetterMd;
    }
    if (record && Object.hasOwn(payload, 'coverLetterMd')) {
        if (!record.coverLetterScope) throw new Error('Обновите карточку вакансии перед редактированием письма.');
        payload.coverLetterScope = record.coverLetterScope;
    }
    if (value.telegramChannelId)
    payload.telegramChannelId = Number(value.telegramChannelId);
else {
    delete payload.telegramChannelId;
    delete payload.telegramMessageUrl;
    delete payload.telegramContact;
} setSaving(true); try {
    const result = await mutate<{
        vacancies: VacancyDetail[];
    }>('/api/vacancies' + (record ? '/' + record.id : ''), record ? 'PUT' : 'POST', payload);
    const saved = result.vacancies.find(v => record ? v.id === record.id : v.url === new URL(value.url).href);
    onSaved(saved?.id);
}
catch (error) {
    if (error instanceof ApiError && [409, 428].includes(error.status)) setConflict(true);
    throw error;
}
finally {
    setSaving(false);
} }}>{busy => <><p className="vacancy-editor-hint">Поля со звёздочкой обязательны. Зарплату можно указать одной или двумя границами.</p><fieldset disabled={busy} style={{ border: 0, padding: 0, minWidth: 0 }}><div className="react-form"><Field label="Название вакансии *"><input required maxLength={300} value={value.title} onChange={e => set('title', e.target.value)}/></Field><Field label="Ссылка на вакансию *"><input type="url" required maxLength={2048} value={value.url} onChange={e => set('url', e.target.value)}/></Field><Field label="Роль *"><VacancyRolePicker selected={value.roleIds} onChange={roleIds=>setValue({...value,roleIds})}/></Field><Field label="Уровень *"><select required value={value.level} onChange={e=>set('level',e.target.value)}><option value="">Выберите…</option>{levels.map(level=><option key={level}>{level}</option>)}</select></Field>{owner.entityType !== 'company' && <Field label="Реальный работодатель"><input maxLength={300} value={value.realCompanyName} onChange={e => set('realCompanyName', e.target.value)}/></Field>}<h3>Зарплата</h3><div className="task-form-grid">{(['salaryFrom', 'salaryTo'] as const).map(k => <Field key={k} label={k === 'salaryFrom' ? 'От, ₽ / месяц' : 'До, ₽ / месяц'}><input type="number" min={0} step={1} value={value[k]} onChange={e => set(k, e.target.value)}/></Field>)}{(['publishedAt', 'loadedAt'] as const).map(k => <Field key={k} label={k === 'publishedAt' ? 'Дата публикации' : 'Дата загрузки'}><input type="date" value={value[k]} onChange={e => set(k, e.target.value)}/></Field>)}</div><Field label="Моя релевантность, %"><input type="number" min={0} max={100} step={1} value={value.relevance} onChange={e => set('relevance', e.target.value)}/></Field><Field label="Полный текст вакансии — Markdown *"><MarkdownEditor label="Полный текст вакансии" value={value.descriptionMd} onChange={v => set('descriptionMd', v)} limit={100000}/></Field><h3>Сопроводительное письмо</h3><p className="vacancy-editor-hint">{record?.coverLetterScope === 'personal' ? 'Личное письмо: изменения видны только вам. Для удаления очистите тему и текст; при следующем открытии письмо будет создано заново.' : 'Общее письмо: текст виден всем пользователям.'}</p><Field label="Тема письма"><input maxLength={512} value={value.coverLetterSubject} onChange={e => set('coverLetterSubject', e.target.value)}/></Field><MarkdownEditor label="Текст письма" value={value.coverLetterMd} onChange={v => set('coverLetterMd', v)} limit={2000}/><h3>Telegram-источник</h3><p className="vacancy-editor-hint">Можно добавить источник или обновить выбранный канал. Остальные источники сохранятся.</p><Field label="Канал"><select required={owner.entityType === 'channel'} value={value.telegramChannelId} onChange={e => { const s = record?.telegramSources.find(s => s.channelId === Number(e.target.value)); setValue({ ...value, telegramChannelId: e.target.value, telegramMessageUrl: s?.messageUrl || '', telegramContact: s?.telegramContact || '' }); }}><option value="">Без нового источника</option>{[...channels.values()].map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</select></Field><Field label="Ссылка на сообщение"><input type="url" maxLength={2048} required={!!value.telegramChannelId} disabled={!value.telegramChannelId} value={value.telegramMessageUrl} onChange={e => set('telegramMessageUrl', e.target.value)}/></Field><Field label="Контакт для связи"><input maxLength={256} disabled={!value.telegramChannelId} value={value.telegramContact} onChange={e => set('telegramContact', e.target.value)}/></Field></div></fieldset>{record && conflict && <VacancyConflict id={record.id} disabled={busy}/>}<div className="vacancy-editor-footer"><button className="vacancy-application-save" type="submit" disabled={busy}>{record ? 'Сохранить изменения' : 'Добавить вакансию'}</button><button className="vacancy-source-button" type="button" disabled={busy} onClick={close}>Отмена</button></div></>}</ActionForm></div></Drawer>; }
