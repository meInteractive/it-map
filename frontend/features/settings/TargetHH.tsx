import { useEffect, useRef, useState } from 'react';
import { mutate, useApi } from '../../api/client';
import { ErrorText, Field, State } from '../../components/Common';

type Source = { companyId: number; companyName: string; enabled: boolean; supported: boolean };
type Pending = { kind: 'start' | 'continue' | 'stage'; requestId: string };
type Progress = { companyId: number; scanRequestId: string; scanJobId?: string; pending?: Pending };
type Result = { jobId?: string; processed: number; errors: number; status: string };
const storageKey = 'itmap-target-hh-v1';
function loadProgress(): Progress | null {
  try {
    const raw = sessionStorage.getItem(storageKey);
    if (!raw) return null;
    const v = JSON.parse(raw) as Progress;
    if (
      !Number.isSafeInteger(v.companyId) ||
      v.companyId < 1 ||
      typeof v.scanRequestId !== 'string' ||
      (v.scanJobId !== undefined && typeof v.scanJobId !== 'string') ||
      (v.pending !== undefined &&
        (!['start', 'continue', 'stage'].includes(v.pending.kind) ||
          typeof v.pending.requestId !== 'string'))
    )
      return null;
    return v;
  } catch {
    return null;
  }
}
function saveProgress(p: Progress | null) {
  try {
    if (p) sessionStorage.setItem(storageKey, JSON.stringify(p));
    else sessionStorage.removeItem(storageKey);
  } catch {
    /* Сервер сохраняет идентификаторы и состояние заданий. */
  }
}
export default function TargetHH() {
  const sources = useApi<{ sources: Source[] }>('/api/apify/hh-sources');
  const [company, setCompany] = useState(''),
    [progress, setProgress] = useState<Progress | null>(loadProgress),
    [busy, setBusy] = useState(false),
    [batchRunning, setBatchRunning] = useState(false),
    [error, setError] = useState<unknown>(null),
    [message, setMessage] = useState('');
  const live = useRef(true),
    busyRef = useRef(false),
    stopRef = useRef(false);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
      stopRef.current = true;
    };
  }, []);
  const keep = (p: Progress | null) => {
    saveProgress(p);
    setProgress(p);
  };
  const execute = async (p: Progress, pending: Pending) => {
    if (busyRef.current) return;
    busyRef.current = true;
    const active = { ...p, pending };
    keep(active);
    setBusy(true);
    setError(null);
    setMessage('');
    const body =
      pending.kind === 'start'
        ? { requestId: pending.requestId, companyId: p.companyId }
        : {
            requestId: pending.requestId,
            scanRequestId: p.scanRequestId,
            scanJobId: p.scanJobId,
            companyId: p.companyId,
          };
    const path =
      pending.kind === 'start'
        ? '/api/scheduler/target-hh'
        : pending.kind === 'continue'
          ? '/api/scheduler/target-hh/continue'
          : '/api/scheduler/target-hh/stage';
    try {
      const result = await mutate<Result>(path, 'POST', body);
      if (!live.current) return;
      if (pending.kind === 'start' && !result.jobId)
        throw new Error('Сервер не вернул идентификатор сканирования. Повторите запрос.');
      const next = { ...p, scanJobId: result.jobId || p.scanJobId };
      delete next.pending;
      keep(next);
      setMessage(
        result.errors
          ? `Этап выполнен с ошибками: ${result.errors}. Подробности доступны в очереди.`
          : result.processed
            ? 'Этап выполнен. Проверьте состояние в очереди.'
            : 'Задание пока не готово или этапов больше нет.',
      );
    } catch (e) {
      if (live.current) setError(e);
    } finally {
      busyRef.current = false;
      if (live.current) setBusy(false);
    }
  };
  const start = () => {
    const id = Number(company);
    if (!Number.isSafeInteger(id) || id < 1) return;
    const requestId = crypto.randomUUID();
    const p = { companyId: id, scanRequestId: requestId };
    void execute(p, { kind: 'start', requestId });
  };
  const next = (kind: 'continue' | 'stage') => {
    if (progress?.scanJobId) void execute(progress, { kind, requestId: crypto.randomUUID() });
  };
  const runStages = async () => {
    if (!progress?.scanJobId || progress.pending || busyRef.current) return;
    busyRef.current = true;
    stopRef.current = false;
    setBusy(true);
    setBatchRunning(true);
    setError(null);
    let p = progress,
      done = 0;
    try {
      for (let i = 0; i < 30; i++) {
        if (stopRef.current || !live.current) {
          if (live.current) setMessage(`Остановлено после этапов: ${done}.`);
          return;
        }
        const pending: Pending = { kind: 'stage', requestId: crypto.randomUUID() };
        keep({ ...p, pending });
        const result = await mutate<Result>('/api/scheduler/target-hh/stage', 'POST', {
          requestId: pending.requestId,
          scanRequestId: p.scanRequestId,
          scanJobId: p.scanJobId,
          companyId: p.companyId,
        });
        if (!live.current) return;
        if (
          !Number.isInteger(result.processed) ||
          result.processed < 0 ||
          result.processed > 1 ||
          !Number.isInteger(result.errors) ||
          result.errors < 0 ||
          typeof result.status !== 'string'
        )
          throw new Error('Неожиданный ответ сервера. Повторите тот же запрос.');
        const next = { ...p };
        delete next.pending;
        p = next;
        keep(p);
        if (result.errors || result.status !== 'completed') {
          setMessage(`Обработка остановлена: выполнено этапов ${done}, проверьте очередь.`);
          return;
        }
        if (result.processed === 0) {
          setMessage(`Выполнено этапов: ${done}. Сейчас доступных этапов больше нет.`);
          return;
        }
        if (result.processed !== 1)
          throw new Error('Неожиданный ответ сервера. Проверьте очередь.');
        done++;
        setMessage(`Выполнено этапов: ${done}.`);
      }
      setMessage(`Выполнено этапов: ${done}. Для продолжения запустите обработку ещё раз.`);
    } catch (e) {
      if (live.current) setError(e);
    } finally {
      busyRef.current = false;
      if (live.current) {
        setBusy(false);
        setBatchRunning(false);
      }
    }
  };
  const selected = sources.data?.sources.find((s) => s.companyId === progress?.companyId);
  return (
    <article className="settings-card">
      <h3>Проверить HH одной компании</h3>
      <p className="resume-hint">
        Используются общие фильтр дат, настройки парсинга и бюджеты. Запуск может расходовать
        средства Apify и выбранных моделей.
      </p>
      <State loading={!sources.data && sources.loading} error={sources.error}>
        {sources.data && (
          <>
            {!progress && (
              <>
                <Field label="Компания">
                  <select
                    className="settings-select"
                    value={company}
                    onChange={(e) => setCompany(e.target.value)}
                  >
                    <option value="">Выберите компанию</option>
                    {sources.data.sources
                      .filter((s) => s.enabled && s.supported)
                      .map((s) => (
                        <option key={s.companyId} value={s.companyId}>
                          {s.companyName}
                        </option>
                      ))}
                  </select>
                </Field>
                <button
                  className="settings-button"
                  type="button"
                  disabled={busy || !company}
                  onClick={start}
                >
                  Запустить проверку HH
                </button>
              </>
            )}
            {progress && (
              <>
                <p>Компания: {selected?.companyName || `#${progress.companyId}`}</p>
                {progress.pending ? (
                  <button
                    className="settings-button secondary"
                    type="button"
                    disabled={busy}
                    onClick={() => void execute(progress, progress.pending!)}
                  >
                    Повторить запрос с тем же идентификатором
                  </button>
                ) : (
                  <>
                    <div className="scheduler-actions">
                      <button
                        className="settings-button secondary"
                        type="button"
                        disabled={busy || !progress.scanJobId}
                        onClick={() => next('continue')}
                      >
                        Продолжить получение списка
                      </button>
                      <button
                        className="settings-button secondary"
                        type="button"
                        disabled={busy || !progress.scanJobId}
                        onClick={() => next('stage')}
                      >
                        Обработать следующий этап
                      </button>
                      <button
                        className="settings-button"
                        type="button"
                        disabled={busy || !progress.scanJobId}
                        onClick={() => void runStages()}
                      >
                        Обработать доступные этапы
                      </button>
                    </div>
                    <button
                      className="settings-button secondary"
                      type="button"
                      disabled={busy}
                      onClick={() => {
                        keep(null);
                        setMessage('');
                        setError(null);
                      }}
                    >
                      Выбрать другую компанию
                    </button>
                  </>
                )}
              </>
            )}
            {busy && <p role="status">Обрабатываем запрос…</p>}
            {batchRunning && (
              <button
                type="button"
                className="settings-button secondary"
                onClick={() => {
                  stopRef.current = true;
                }}
              >
                Остановить после текущего этапа
              </button>
            )}
            <p role="status">{message}</p>
            <ErrorText error={error} />
          </>
        )}
      </State>
    </article>
  );
}
