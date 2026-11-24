import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { VacancyDetail } from '../../../shared/contracts';
import { ErrorText, Icon } from '../../components/Common';
import { Markdown } from '../../components/Markdown';
import { renderedClipboard, writeRichClipboard } from '../../lib/clipboard';
import { useResumeSelection } from '../settings/ResumeSelect';
import { generateLetter, useCoverLetter } from './cover-letter';

export function CopyLetterButton({
  content,
  disabled,
  label,
}: {
  content: () => HTMLElement | null;
  disabled: boolean;
  label: string;
}) {
  const [status, setStatus] = useState<'idle' | 'pending' | 'copied' | 'error'>('idle');
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      clearTimeout(timer.current);
    };
  }, []);
  async function copy() {
    if (status !== 'idle') return;
    const element = content();
    if (!element) return;
    const { plain, html } = renderedClipboard(element);
    setStatus('pending');
    try {
      await writeRichClipboard(plain, html);
      if (mounted.current) setStatus('copied');
    } catch {
      if (mounted.current) setStatus('error');
    } finally {
      if (mounted.current) timer.current = setTimeout(() => setStatus('idle'), 1400);
    }
  }
  return (
    <button
      className="cover-letter-copy"
      type="button"
      aria-label={label}
      disabled={disabled || status !== 'idle'}
      onClick={() => void copy()}
    >
      <Icon name="clipboardText" />
      <span aria-live="polite">
        {status === 'copied' ? 'Скопировано' : status === 'error' ? 'Не удалось' : 'Копировать'}
      </span>
    </button>
  );
}

const customPromptLimit = 5000;

export default function CoverLetter({
  v,
  admin,
  ready = true,
  sourceAction,
}: {
  v: VacancyDetail;
  admin: boolean;
  ready?: boolean;
  sourceAction?: ReactNode;
}) {
  const state = useCoverLetter(v.id);
  const resume = useResumeSelection();
  const [customPrompt, setCustomPrompt] = useState('');
  const promptLength = Array.from(customPrompt).length;
  const canGenerate = ready && !state.running && (!admin || promptLength <= customPromptLimit);
  function generate() {
    if (!canGenerate) return;
    void generateLetter(v.id, {
      regenerate: !!v.coverLetterSubject?.trim() && !!v.coverLetterMd?.trim(),
      resumeId: resume.id,
      customPrompt: admin ? customPrompt.trim() : '',
    });
  }
  // Saved text has one source: the current detail response, including empty text.
  const letter = v;
  const subject = useRef<HTMLDivElement>(null),
    text = useRef<HTMLDivElement>(null);
  return (
    <section className="cover-letter">
      {resume.control}
      {admin && (
        <label className="cover-letter-prompt">
          <span>Пользовательский промт</span>
          <textarea
            rows={4}
            value={customPrompt}
            onChange={(event) => setCustomPrompt(event.target.value)}
            placeholder="Укажите кейс из портфолио, на котором стоит сделать акцент"
            aria-invalid={promptLength > customPromptLimit}
          />
          <small>{promptLength.toLocaleString('ru-RU')} / 5 000</small>
        </label>
      )}
      <div className="cover-letter-actions">
        {sourceAction}
        <button
          className="vacancy-source-button"
          type="button"
          aria-label={
            letter.coverLetterMd
              ? 'Создать сопроводительное письмо заново'
              : 'Создать сопроводительное письмо'
          }
          title="Создать письмо по текущим настройкам. При выборе OpenAI API-запрос может быть платным."
          disabled={!canGenerate}
          onClick={generate}
        >
          Создать
        </button>
      </div>
      <div
        className="vacancy-application-loading"
        role="status"
        aria-live="polite"
        hidden={!state.message}
      >
        {state.message}
      </div>
      <ErrorText error={state.error} />
      {state.error && !v.coverLetterMd && (
        <button
          className="vacancy-source-button"
          type="button"
          disabled={!canGenerate}
          onClick={generate}
        >
          Повторить
        </button>
      )}
      <article className="cover-letter-card">
        <div className="cover-letter-heading">
          <h3>Заголовок письма</h3>
          <CopyLetterButton
            content={() => subject.current}
            disabled={!letter.coverLetterSubject}
            label="Скопировать заголовок сопроводительного письма"
          />
        </div>
        <div className="cover-letter-subject" ref={subject}>
          {letter.coverLetterSubject ? (
            <span>{letter.coverLetterSubject}</span>
          ) : (
            <div className="cover-letter-empty">Заголовок пока не создан.</div>
          )}
        </div>
      </article>
      <article className="cover-letter-card">
        <div className="cover-letter-heading">
          <h3>Текст письма</h3>
          <CopyLetterButton
            content={() => text.current}
            disabled={!letter.coverLetterMd}
            label="Скопировать текст сопроводительного письма с форматированием"
          />
        </div>
        <div className="vacancy-drawer-md cover-letter-text" ref={text}>
          {letter.coverLetterMd ? (
            <Markdown className="markdown-content" value={letter.coverLetterMd} />
          ) : (
            <div className="cover-letter-empty">Письмо пока не создано.</div>
          )}
        </div>
      </article>
    </section>
  );
}
