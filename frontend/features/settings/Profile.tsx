import { ClassificationPrompts } from './Classification';
import { useState, type ReactNode } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, State, dateLabel } from '../../components/Common';
import { MarkdownEditor } from '../../components/Markdown';
import { PromptModel, type PromptModelSettings } from './PromptModel';
import type { Resumes } from './ResumeSelect';
import { useDirty } from '../../hooks/useDirty';
interface RevisionText {
  revision: number;
  updatedAt: string;
  resumeMd?: string;
  portfolioMd?: string;
  maxLength?: number;
  markdown?: string;
}
export function RevisionEditor({
  initial,
  url,
  field,
  limit,
  title,
  description,
  children,
  defaultValue,
}: {
  children?: ReactNode;
  defaultValue?: string;
  initial: RevisionText;
  url: string;
  field: 'resumeMd' | 'portfolioMd' | 'markdown';
  limit: number;
  title: string;
  description: string;
}) {
  const [value, setValue] = useState(initial[field] || ''),
    [saved, setSaved] = useState(initial[field] || ''),
    [revision, setRevision] = useState(initial.revision),
    [updated, setUpdated] = useState(initial.updatedAt),
    [message, setMessage] = useState('');
  useDirty(value !== saved);
  return (
    <article className="resume-card">
      <h2>{title}</h2>
      <p className="resume-hint">{description}</p>
      <ActionForm
        className="resume-form"
        onSubmit={async () => {
          if (Array.from(value).length > limit)
            throw new Error('Превышен лимит ' + limit + ' символов.');
          const sent = value;
          const result = await mutate<RevisionText>(url, 'PUT', { [field]: sent, revision });
          setSaved(result[field] || '');
          setRevision(result.revision);
          setUpdated(result.updatedAt);
          setMessage('Сохранено.');
        }}
      >
        {(busy) => (
          <>
            <MarkdownEditor
              label={title}
              value={value}
              onChange={(v) => {
                setValue(v);
                setMessage('');
              }}
              limit={limit}
              className="resume-editor"
              footer={
                <button
                  type="submit"
                  className="settings-button"
                  disabled={busy || value === saved || Array.from(value).length > limit}
                >
                  {field === 'markdown' ? 'Сохранить' : 'Сохранить ' + title.toLowerCase()}
                </button>
              }
            />
            <button
              type="button"
              className="settings-button secondary"
              hidden={defaultValue === undefined}
              onClick={() => {
                if (defaultValue !== undefined) {
                  setValue(defaultValue);
                  setMessage('Стандартный текст восстановлен в редакторе. Сохраните промт.');
                }
              }}
            >
              Восстановить стандартный промт
            </button>
            <div className={'resume-status' + (message ? ' success' : '')} role="status">
              {message || (value !== saved ? 'Есть несохранённые изменения.' : 'Изменений нет.')}
            </div>
            <div className="resume-status">Обновлено: {dateLabel(updated)}</div>
          </>
        )}
      </ActionForm>
      {children}
    </article>
  );
}
export function Resume() {
  const q = useApi<Resumes>('/api/profile/resumes');
  return (
    <>
      <State loading={!q.data && q.loading} error={q.error}>
        {q.data && <ResumeCollection initial={q.data} />}
      </State>
      {q.error && <button onClick={q.reload}>Повторить загрузку резюме</button>}
      <Portfolio />
    </>
  );
}
function ResumeCollection({ initial }: { initial: Resumes }) {
  const [saved, setSaved] = useState(initial);
  const [draft, setDraft] = useState(initial);
  const [active, setActive] = useState(initial.primary);
  const [message, setMessage] = useState('');
  const item = draft.items.find((resume) => resume.id === active) ?? draft.items[0];
  const dirty = draft !== saved;
  const invalid = draft.items.some(
    (resume) =>
      !resume.name.trim() ||
      Array.from(resume.name).length > 120 ||
      Array.from(resume.markdown).length > 20000,
  );
  useDirty(dirty);
  function changeItem(patch: Partial<Pick<Resumes['items'][number], 'name' | 'markdown'>>) {
    setMessage('');
    setDraft({
      ...draft,
      items: draft.items.map((resume) => (resume.id === active ? { ...resume, ...patch } : resume)),
    });
  }
  return (
    <article className="resume-card">
      <h2>Резюме</h2>
      <ActionForm
        lockWhileBusy
        className="resume-form"
        onSubmit={async () => {
          if (invalid) throw new Error('Проверьте названия и объём резюме.');
          const result = await mutate<Resumes>('/api/profile/resumes', 'PUT', draft);
          setSaved(result);
          setDraft(result);
          setMessage('Сохранено.');
        }}
      >
        {(busy) => (
          <>
            <label className="crm-field">
              <span>Редактируемое резюме</span>
              <select value={item.id} onChange={(event) => setActive(Number(event.target.value))}>
                {draft.items.map((resume) => (
                  <option key={resume.id} value={resume.id}>
                    {resume.name || 'Без названия'}
                  </option>
                ))}
              </select>
            </label>
            <button
              type="button"
              className="settings-button secondary"
              disabled={busy || draft.items.length >= 3}
              onClick={() => {
                const id = draft.items.length + 1;
                setDraft({
                  ...draft,
                  items: [...draft.items, { id, name: 'Резюме ' + id, markdown: '' }],
                });
                setActive(id);
                setMessage('');
              }}
            >
              Добавить резюме
            </button>
            <label className="crm-field">
              <span>Название</span>
              <input
                value={item.name}
                onChange={(event) => changeItem({ name: event.target.value })}
              />
            </label>
            <label>
              <input
                type="radio"
                checked={draft.primary === item.id}
                onChange={() => {
                  setDraft({ ...draft, primary: item.id });
                  setMessage('');
                }}
              />{' '}
              Основное резюме
            </label>
            <MarkdownEditor
              key={item.id}
              label="Текст резюме"
              value={item.markdown}
              onChange={(markdown) => changeItem({ markdown })}
              limit={20000}
              className="resume-editor"
              footer={
                <button
                  type="submit"
                  className="settings-button"
                  disabled={busy || !dirty || invalid}
                >
                  Сохранить резюме
                </button>
              }
            />
            {message && (
              <div className="resume-status success" role="status">
                {message}
              </div>
            )}
          </>
        )}
      </ActionForm>
    </article>
  );
}
function Portfolio() {
  const q = useApi<RevisionText>('/api/profile/portfolio');
  return (
    <>
      <State loading={!q.data && q.loading} error={q.error}>
        {q.data && (
          <RevisionEditor
            initial={q.data}
            url="/api/profile/portfolio"
            field="portfolioMd"
            limit={q.data.maxLength ?? 50000}
            title="Портфолио"
            description="Описание реализованных проектов для сопроводительных писем. Для каждого проекта используйте подзаголовки «Название», «Предметная область», «Технические детали» и «Описание»."
          />
        )}
      </State>
      {q.error && <button onClick={q.reload}>Повторить загрузку портфолио</button>}
    </>
  );
}
export function Prompts() {
  const settings = useApi<{ models: Record<string, PromptModelSettings> }>(
    '/api/profile/prompt-models',
  );
  const q = useApi<{
    prompts: Record<string, RevisionText>;
    maxLengths?: Record<string, number>;
    defaults?: Record<string, string>;
  }>('/api/profile/prompts');
  return (
    <section className="prompts-settings">
      <State loading={!q.data && q.loading} error={q.error}>
        {q.data && (
          <div className="prompts-grid">
            {Object.entries({
              cover_letter: 'Сопроводительное письмо',
              relevance: 'Релевантность',
              vacancy_extraction: 'Структурирование вакансии',
              vacancy_role: 'Определение роли вакансии',
            }).map(([kind, title]) => (
              <RevisionEditor
                key={kind}
                initial={q.data!.prompts[kind]}
                url={'/api/profile/prompts/' + kind}
                field="markdown"
                defaultValue={q.data!.defaults?.[kind]}
                limit={q.data!.maxLengths?.[kind] || 5000}
                title={title}
                description="Общий системный промт для всех пользователей. Редактирование доступно только администратору."
              >
                {settings.error && (
                  <button className="settings-button secondary" onClick={settings.reload}>
                    Повторить загрузку настроек модели
                  </button>
                )}
                <State loading={!settings.data && settings.loading} error={settings.error}>
                  {settings.data?.models?.[kind] && (
                    <PromptModel initial={settings.data.models[kind]} />
                  )}
                </State>
              </RevisionEditor>
            ))}
          </div>
        )}
      </State>
      <ClassificationPrompts />
    </section>
  );
}
