import {ClassificationPrompts} from './Classification';
import { useState, type ReactNode } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, State, dateLabel } from '../../components/Common';
import { MarkdownEditor } from '../../components/Markdown';
import {PromptModel,type PromptModelSettings} from './PromptModel';
import { useDirty } from '../../hooks/useDirty';
interface RevisionText {
    revision: number;
    updatedAt: string;
    resumeMd?: string;
    markdown?: string;
}
export function RevisionEditor({ initial, url, field, limit, title, description, children, defaultValue }: {
    children?: ReactNode;
    defaultValue?: string;
    initial: RevisionText;
    url: string;
    field: 'resumeMd' | 'markdown';
    limit: number;
    title: string;
    description: string;
}) { const [value, setValue] = useState(initial[field] || ''), [saved, setSaved] = useState(initial[field] || ''), [revision, setRevision] = useState(initial.revision), [updated, setUpdated] = useState(initial.updatedAt), [message, setMessage] = useState(''); useDirty(value !== saved); return <article className="resume-card"><h2>{title}</h2><p className="resume-hint">{description}</p><ActionForm className="resume-form" onSubmit={async () => { if (Array.from(value).length > limit)
    throw new Error('Превышен лимит ' + limit + ' символов.'); const sent = value; const result = await mutate<RevisionText>(url, 'PUT', { [field]: sent, revision }); setSaved(result[field] || ''); setRevision(result.revision); setUpdated(result.updatedAt); setMessage('Сохранено.'); }}>{busy => <><MarkdownEditor label={title} value={value} onChange={v => { setValue(v); setMessage(''); }} limit={limit} className="resume-editor"
    footer={<button type="submit" className="settings-button" disabled={busy || value === saved || Array.from(value).length > limit}>
        {field === 'resumeMd' ? 'Сохранить резюме' : 'Сохранить'}
    </button>}/>
    <button type="button" className="settings-button secondary" hidden={defaultValue===undefined} onClick={()=>{if(defaultValue!==undefined){setValue(defaultValue);setMessage('Стандартный текст восстановлен в редакторе. Сохраните промт.');}}}>Восстановить стандартный промт</button>
    <div className={'resume-status' + (message ? ' success' : '')} role="status">
        {message || (value !== saved ? 'Есть несохранённые изменения.' : 'Изменений нет.')}
    </div>
    <div className="resume-status">Обновлено: {dateLabel(updated)}</div></>}</ActionForm>{children}</article>; }
export function Resume() { const q = useApi<RevisionText>('/api/profile/resume'); return <State loading={!q.data && q.loading} error={q.error}>{q.error && <button onClick={q.reload}>Повторить</button>}{q.data && <RevisionEditor initial={q.data} url="/api/profile/resume" field="resumeMd" limit={20000} title="Резюме" description="Ваше личное резюме в Markdown. Используется для оценки релевантности вакансий и сопроводительных писем."/>}</State>; }
export function Prompts() { const settings=useApi<{models:Record<string,PromptModelSettings>}>('/api/profile/prompt-models'); const q = useApi<{
    prompts: Record<string, RevisionText>;
    maxLengths?: Record<string, number>;
    defaults?: Record<string,string>;
}>('/api/profile/prompts'); return <section className="prompts-settings"><State loading={!q.data && q.loading} error={q.error}>{q.data && <div className="prompts-grid">{Object.entries({ cover_letter: 'Сопроводительное письмо', relevance: 'Релевантность', vacancy_extraction: 'Структурирование вакансии', vacancy_role: 'Определение роли вакансии' }).map(([kind, title]) => <RevisionEditor key={kind} initial={q.data!.prompts[kind]} url={'/api/profile/prompts/' + kind} field="markdown" defaultValue={q.data!.defaults?.[kind]} limit={q.data!.maxLengths?.[kind] || 5000} title={title} description="Общий системный промт для всех пользователей. Редактирование доступно только администратору.">{settings.error&&<button className="settings-button secondary" onClick={settings.reload}>Повторить загрузку настроек модели</button>}<State loading={!settings.data&&settings.loading} error={settings.error}>{settings.data?.models?.[kind]&&<PromptModel initial={settings.data.models[kind]}/>}</State></RevisionEditor>)}</div>}</State><ClassificationPrompts/></section>; }
