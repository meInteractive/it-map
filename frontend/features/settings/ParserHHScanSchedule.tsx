import { useApi } from '../../api/client';
import { State } from '../../components/Common';
import CopyDebugJSON from './CopyDebugJSON';
import { hhScanScheduleSnapshot } from '../../../shared/parser-statistics-debug.mjs';

interface HHScheduleSummary {
  configured: number;
  disabled: number;
  outsideScope: number;
  activeScan: number;
  waiting: number;
  potentiallyDue: number;
  recalculated: number;
  unknownLastSuccess: number;
  successfulLast24h?: number;
  successfulLast48h?: number;
  successDateUnknown?: number;
  noSuccess48hWaiting?: number;
  noSuccess48hActive?: number;
  noSuccess48hDue?: number;
  noSuccess48hWaitPartial?: number;
  nextDueAt: number | null;
}
interface HHPendingBreakdown {
  pending: number;
  notDue: number;
  unverifiedCursor: number;
  unverifiedLineage?: number;
  missingOperation?: number;
  orphanPaid: number;
  reconcileCandidate: number;
  launchCooldown: number;
  launchCandidate: number;
  other: number;
  oldestPendingAt: number | null;
  oldestUnverifiedAt?: number | null;
  oldestReconcileAt?: number | null;
  oldestCooldownAt?: number | null;
}
export interface HHScanScheduleReport {
  checkedAt: number;
  sourceId: 'hh';
  enabled: boolean;
  sourceSelected: boolean;
  favoritesOnly: boolean;
  intervalDays: 1 | 3 | 7;
  vacancyAgeDays: 0 | 3 | 7 | 14;
  summary: HHScheduleSummary;
  pendingHHScans: number;
  runningHHScans: number;
  pendingBreakdown?: HHPendingBreakdown;
  finalEligibilityVerified: false;
}
const nonnegative = (v: unknown): v is number =>
  typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
const object = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === 'object' && !Array.isArray(v);
// A named type guard carries the numeric runtime checks into the arithmetic
// below. A dynamic keys.every alone cannot narrow Record<string, unknown>.
const isHHScheduleSummary = (v: unknown): v is HHScheduleSummary => {
  if (!object(v)) return false;
  const keys = [
    'configured',
    'disabled',
    'outsideScope',
    'activeScan',
    'waiting',
    'potentiallyDue',
    'recalculated',
    'unknownLastSuccess',
  ];
  const optional = [
    'successfulLast24h', 'successfulLast48h', 'successDateUnknown',
    'noSuccess48hWaiting', 'noSuccess48hActive', 'noSuccess48hDue',
    'noSuccess48hWaitPartial',
  ];
  return (
    keys.every((k) => nonnegative(v[k])) &&
    optional.every((k) => v[k] === undefined || nonnegative(v[k])) &&
    (v.nextDueAt === null || nonnegative(v.nextDueAt))
  );
};
const isHHPendingBreakdown = (v: unknown): v is HHPendingBreakdown => {
  if (!object(v)) return false;
  const fields = [
    'notDue',
    'unverifiedCursor',
    'orphanPaid',
    'reconcileCandidate',
    'launchCooldown',
    'launchCandidate',
    'other',
  ];
  return (
    nonnegative(v.pending) &&
    fields.every((key) => nonnegative(v[key])) &&
    fields.reduce((sum, key) => sum + Number(v[key]), 0) === v.pending &&
    (v.oldestPendingAt === null || nonnegative(v.oldestPendingAt)) &&
    ['oldestUnverifiedAt', 'oldestReconcileAt', 'oldestCooldownAt'].every((key) =>
      v[key] === undefined || v[key] === null || nonnegative(v[key])) &&
    (v.unverifiedLineage === undefined && v.missingOperation === undefined ||
      nonnegative(v.unverifiedLineage) && nonnegative(v.missingOperation) &&
      v.unverifiedLineage + v.missingOperation === v.unverifiedCursor)
  );
};
export function isHHScanScheduleReport(v: unknown): v is HHScanScheduleReport {
  if (
    !object(v) ||
    !nonnegative(v.checkedAt) ||
    v.sourceId !== 'hh' ||
    typeof v.enabled !== 'boolean' ||
    typeof v.sourceSelected !== 'boolean' ||
    typeof v.favoritesOnly !== 'boolean' ||
    ![1, 3, 7].includes(v.intervalDays as number) ||
    ![0, 3, 7, 14].includes(v.vacancyAgeDays as number) ||
    v.finalEligibilityVerified !== false ||
    !isHHScheduleSummary(v.summary) ||
    !nonnegative(v.pendingHHScans) ||
    !nonnegative(v.runningHHScans) ||
    (v.pendingBreakdown !== undefined &&
      (!isHHPendingBreakdown(v.pendingBreakdown) ||
        v.pendingBreakdown.pending !== v.pendingHHScans))
  )
    return false;
  const s = v.summary;
  const eligible = s.configured - s.disabled - s.outsideScope;
  const coveragePresent =
    s.successfulLast24h !== undefined ||
    s.successfulLast48h !== undefined ||
    s.successDateUnknown !== undefined;
  const coverageValid =
    !coveragePresent ||
    (s.successfulLast24h !== undefined &&
      s.successfulLast48h !== undefined &&
      s.successDateUnknown !== undefined &&
      s.successfulLast24h <= s.successfulLast48h &&
      s.successfulLast48h + s.successDateUnknown <= eligible);
  const noSuccessKeys = [
    'noSuccess48hWaiting', 'noSuccess48hActive', 'noSuccess48hDue',
    'noSuccess48hWaitPartial',
  ] as const;
  const noSuccessPresent = noSuccessKeys.some((key) => s[key] !== undefined);
  const noSuccessValid = !noSuccessPresent || (
    noSuccessKeys.every((key) => nonnegative(s[key])) &&
    s.successfulLast48h !== undefined &&
    s.noSuccess48hWaiting! + s.noSuccess48hActive! + s.noSuccess48hDue! ===
      eligible - s.successfulLast48h &&
    s.noSuccess48hWaitPartial! <= s.noSuccess48hWaiting! &&
    s.noSuccess48hWaiting! <= s.waiting && s.noSuccess48hActive! <= s.activeScan &&
    s.noSuccess48hDue! <= s.potentiallyDue
  );
  return (
    coverageValid && noSuccessValid &&
    s.disabled + s.outsideScope + s.activeScan + s.waiting + s.potentiallyDue === s.configured &&
    s.waiting > 0 === (s.nextDueAt !== null) &&
    (s.nextDueAt === null || s.nextDueAt > v.checkedAt)
  );
}
const n = (v: number) => v.toLocaleString('ru-RU');
const moscow = (ms: number) =>
  new Date(ms).toLocaleString('ru-RU', {
    timeZone: 'Europe/Moscow',
    dateStyle: 'medium',
    timeStyle: 'short',
  });
export function HHScanScheduleView({ report: r }: { report: HHScanScheduleReport }) {
  const s = r.summary;
  const status = !r.enabled
    ? 'Диспетчер на паузе: новые HH-задания не ставятся.'
    : !r.sourceSelected
      ? 'Источник HH не выбран в общих настройках.'
      : !s.configured
        ? 'Для компаний не настроено ни одного источника HH.'
        : s.potentiallyDue > 0
          ? 'Срок проверки наступил у ' +
            n(s.potentiallyDue) +
            ' источников. Это предварительные кандидаты на постановку, а не гарантия запуска.'
          : s.activeScan > 0
            ? 'Есть активные HH-сканирования. Повторное задание для тех же компаний не ставится.'
            : s.waiting > 0
              ? 'Подходящие по области источники пока ждут следующего срока.'
              : 'Сейчас нет HH-источников, подходящих по области и времени проверки.';
  return (
    <>
      <p role="status">
        <strong>{status}</strong>
      </p>
      <p className="resume-hint">
        Сохранённые правила: раз в {r.intervalDays} дн., вакансии за{' '}
        {r.vacancyAgeDays === 0 ? 'все даты' : r.vacancyAgeDays + ' полных дн.'},{' '}
        {r.favoritesOnly ? 'только избранные компании' : 'все компании'}.
      </p>
      {r.intervalDays > r.vacancyAgeDays && r.vacancyAgeDays !== 0 && (
        <p role="alert" className="error">
          Интервал проверки больше окна публикаций. Между сканированиями часть вакансий может быть
          пропущена.
        </p>
      )}
      <div className="parser-funnel-totals">
        <div>
          <span>Потенциально пора проверить</span>
          <strong>{n(s.potentiallyDue)}</strong>
        </div>
        <div>
          <span>Ожидают срока</span>
          <strong>{n(s.waiting)}</strong>
          {s.nextDueAt !== null && <span>Ближайший: {moscow(s.nextDueAt)} МСК</span>}
        </div>
        <div>
          <span>Уже есть HH-задание</span>
          <strong>{n(s.activeScan)}</strong>
        </div>
      </div>
      <p className="resume-hint">
        Сохранённых HH-источников: {n(s.configured)}; выключено: {n(s.disabled)}; исключено
        настройкой избранного: {n(s.outsideScope)}.
      </p>
      {s.successfulLast24h !== undefined && (
        <p className="resume-hint">
          Успешно проверено HH-источников за последние 24 часа: <strong>{n(s.successfulLast24h)}</strong>{' '}
          из {n(s.configured - s.disabled - s.outsideScope)} включённых в выбранной области.
          За 48 часов — {n(s.successfulLast48h ?? 0)}; без достоверной даты последней успешной
          проверки — {n(s.successDateUnknown ?? 0)}. Учитывается последний полностью завершённый
          обход каждого источника, не количество запусков Apify и не число новых вакансий.
        </p>
      )}
      {s.noSuccess48hWaiting !== undefined && (
        <p className="resume-hint">
          Без подтверждённого полного обхода за 48 часов: ожидают сохранённого срока —{' '}
          <strong>{n(s.noSuccess48hWaiting)}</strong> (из них после частичного результата —{' '}
          {n(s.noSuccess48hWaitPartial ?? 0)}); имеют HH-задание — {n(s.noSuccess48hActive ?? 0)};
          срок наступил без задания — {n(s.noSuccess48hDue ?? 0)}.
          Эти числа не означают, что повторный платный запуск безопасен.
        </p>
      )}
      <p className="resume-hint">
        В очереди текущего поколения: HH ожидают — {n(r.pendingHHScans)}, выполняются —{' '}
        {n(r.runningHHScans)}. Это все локальные HH-задания, включая те, которые автоматический
        worker может не допустить.
      </p>
      {r.pendingBreakdown && (
        <div>
          <p className="resume-hint">
            Предварительная классификация {n(r.pendingBreakdown.pending)} ожидающих HH-заданий:
            срок выполнения ещё не наступил — {n(r.pendingBreakdown.notDue)};
            неподтверждённый курсор — {n(r.pendingBreakdown.unverifiedCursor)};
            существующий запуск для дочитывания — {n(r.pendingBreakdown.reconcileCandidate)};
            курсор с неподтверждённой записью запуска — {n(r.pendingBreakdown.orphanPaid)};
            ограничение частоты нового запуска — {n(r.pendingBreakdown.launchCooldown)};
            потенциально готовы к новому запуску — {n(r.pendingBreakdown.launchCandidate)};
            прочие причины — {n(r.pendingBreakdown.other)}.
          </p>
          {r.pendingBreakdown.unverifiedLineage !== undefined && (
            <p className="resume-hint">
              Неподтверждённый курсор: происхождение или версия источника не подтверждены —{' '}
              {n(r.pendingBreakdown.unverifiedLineage)}; ключ оплаченной операции не восстановлен —{' '}
              {n(r.pendingBreakdown.missingOperation ?? 0)}.
              {r.pendingBreakdown.oldestUnverifiedAt != null && (
                <> Самое старое заблокированное: {moscow(r.pendingBreakdown.oldestUnverifiedAt)} МСК.</>
              )}
              {r.pendingBreakdown.oldestReconcileAt != null && (
                <> Старейшее дочитывание Actor: {moscow(r.pendingBreakdown.oldestReconcileAt)} МСК.</>
              )}
            </p>
          )}
          {r.pendingBreakdown.oldestPendingAt !== null && (
            <p className="resume-hint">
              Самое старое из ожидающих HH-заданий создано: {moscow(r.pendingBreakdown.oldestPendingAt)} МСК.
            </p>
          )}
          <p className="resume-hint">
            Это оценка на момент чтения. Она не проверяет квоты Apify, бюджет, lease, лимиты цикла
            и фактическое разрешение на запуск. Для разблокировки не следует вручную повторять Actor.
          </p>
        </div>
      )}
      {s.recalculated > 0 && (
        <p className="resume-hint">
          После изменения интервала пересчитан срок {n(s.recalculated)} источников на основе
          последней успешной проверки. Сам отчёт ничего не сохраняет.
        </p>
      )}
      {s.unknownLastSuccess > 0 && (
        <p role="note">
          У {n(s.unknownLastSuccess)} источников изменился интервал, но дата последней успешной
          проверки отсутствует или некорректна. Использован сохранённый срок; требуется
          дополнительная проверка.
        </p>
      )}
      <p className="resume-hint">
        Проверено: {moscow(r.checkedAt)} МСК. Диагностика использует существующие таблицы, только
        чтение. Она не проверяет доступность URL, полноту HH-профиля и внешние API. «Пора проверить»
        не означает, что провайдер уже запущен или что вакансия найдена.
      </p>
    </>
  );
}
export default function ParserHHScanSchedule({ isAuthorized }: { isAuthorized?: () => boolean } = {}) {
  const q = useApi<HHScanScheduleReport>('/api/scheduler/hh-schedule', {
    refreshIntervalMs: 60000,
  });
  const report = isHHScanScheduleReport(q.data) && !q.error ? q.data : undefined;
  const invalid = q.data !== undefined && !report && !q.loading && !q.error;
  const error = q.error ?? (invalid ? new Error('Некорректный ответ диагностики HH.') : undefined);
  return (
    <article className="settings-card">
      <div className="parser-line">
        <div>
          <h2>Когда HH проверят снова?</h2>
          <p className="resume-hint">
            Планировщик · сохранённые сроки и фильтры без запуска сканирования
          </p>
        </div>
        <div className="scheduler-actions">
          <CopyDebugJSON
            section="parser_hh_schedule"
            isAuthorized={isAuthorized ?? (() => false)}
            disabled={!report || !isAuthorized?.()}
            getSnapshot={() =>
              report
                ? hhScanScheduleSnapshot(report)
                : { available: false, errorCode: 'hh_schedule_unavailable' }
            }
          />
          <button type="button" className="settings-button secondary" onClick={q.reload}>
            Обновить
          </button>
        </div>
      </div>
      <State loading={!report && q.loading} error={error}>
        {report ? (
          <HHScanScheduleView report={report} />
        ) : (
          !q.loading && !error && <p>Диагностика пока недоступна.</p>
        )}
      </State>
    </article>
  );
}
