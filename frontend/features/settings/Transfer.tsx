import { useRef, useState } from 'react';
import { mutate, clearCache } from '../../api/client';
import { ActionForm, ErrorText } from '../../components/Common';
import { importHelp, backupRestoreHelp } from './TransferHelp';
async function download(url: string, fallback: string) { const response = await fetch(url, { credentials: 'same-origin' }); if (!response.ok) {
    const body = await response.json() as {
        error?: string;
    };
    throw new Error(body.error || 'Не удалось скачать файл.');
} const blob = await response.blob(), href = URL.createObjectURL(blob), a = document.createElement('a'); a.href = href; a.download = response.headers.get('content-disposition')?.match(/filename="([^"]+)"/)?.[1] || fallback; a.click(); setTimeout(() => URL.revokeObjectURL(href), 1000); }
export function ExportCard({ title, description, url, backup = false, unranked = false }: {
    title: string;
    description: string;
    url: string;
    backup?: boolean;
    unranked?: boolean;
}) { const [only, setOnly] = useState(false), [message, setMessage] = useState(''); return <article className="settings-card"><h2>{title}</h2><p>{description}</p><ActionForm onSubmit={async () => { await download(url + (only ? '?unranked=1' : ''), backup ? 'it-employers-backup.json' : 'it-employers.xlsx'); setMessage('Файл скачан.'); }}>{busy => <>{unranked && <label className="export-option"><input type="checkbox" checked={only} onChange={e => setOnly(e.target.checked)}/>Только компании без рейтинга</label>}<div className="settings-actions"><button className="settings-button" type="submit" disabled={busy}>{backup ? 'Скачать бэкап' : 'Скачать Excel'}</button></div><div className="backup-state" role="status">{message}</div></>}</ActionForm></article>; }
interface ImportResult {
    [key: string]: unknown;
    errors?: {
        row: number;
        sheet?: string;
        message: string;
    }[];
    duplicateDetails?: {
        row: number;
        name: string;
        reason: string;
    }[];
}
export function ImportCard({ vacancies = false }: {
    vacancies?: boolean;
}) { const help = vacancies ? importHelp.vacancies : importHelp.companies; const [file, setFile] = useState<File>(), [mode, setMode] = useState('create'), [result, setResult] = useState<ImportResult>(); const labels: Record<string, string> = { added: 'Компаний добавлено', updated: 'Обновлено', notAdded: 'Не обработано', agenciesAdded: 'Агентств добавлено', agenciesUpdated: 'Агентств обновлено', agenciesSkipped: 'Агентств не обработано', contactsAdded: 'Контактов добавлено', contactsUpdated: 'Контактов обновлено', contactsSkipped: 'Контактов не обработано', duplicates: 'Дубликатов пропущено', invalid: 'Строк с ошибками', officesAdded: 'Офисов добавлено', officesUpdated: 'Офисов обновлено', officesSkipped: 'Офисов не обработано', reviewsUpdated: 'Строк отзывов обновлено', reviewsSkipped: 'Строк отзывов не обработано', imported: 'Вакансий добавлено', missingSkipped: 'Новых вакансий пропущено' }; return <article className={'settings-card import-card ' + (vacancies ? 'vacancy-import-card' : 'company-import-card')}><h2>{help.title}</h2><p>{help.description}</p><ActionForm onSubmit={async () => { if (!file)
    throw new Error('Выберите Excel-файл.'); const parser = await import('./xlsx'); const body = vacancies ? { rows: await parser.parseVacancyWorkbook(file), mode } : { ...await parser.parseCompanyWorkbook(file), mode }; setResult(await mutate<ImportResult>('/api/' + (vacancies ? 'vacancies' : 'companies') + '/import', 'POST', body)); }}>{busy => <><fieldset className="vacancy-import-mode" aria-label={vacancies ? "Режим импорта вакансий" : "Режим импорта компаний"}><legend>Режим импорта</legend>{help.modes.map(([key, label, description]) => <label className="vacancy-import-mode-option" key={key}><input type="radio" name={vacancies ? 'vacancyMode' : 'companyMode'} value={key} checked={mode === key} onChange={() => setMode(key)}/><b>{label}</b><small>{description}</small></label>)}</fieldset><input className="settings-file-input" type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" disabled={busy} onChange={e => { setFile(e.target.files?.[0]); setResult(undefined); }}/><button type="submit" className="settings-button restore" disabled={busy || !file}>Импортировать</button></>}</ActionForm><div className="backup-warning">{help.warning}</div>{result && <div className="vacancy-import-report show" role="status"><b>Импорт завершён.</b>{Object.entries(labels).filter(([k]) => typeof result[k] === 'number').map(([k, label]) => <div key={k}>{label}: {String(result[k])}</div>)}{!!result.errors?.length && <details><summary>Показать ошибки</summary><ul>{result.errors.map((e, i) => <li key={i}>{e.sheet} · строка {e.row}: {e.message}</li>)}</ul></details>}{!!result.duplicateDetails?.length && <details><summary>Показать дубликаты</summary><ul>{result.duplicateDetails.map((e, i) => <li key={i}>Строка {e.row}: {e.name} — {e.reason}</li>)}</ul></details>}</div>}</article>; }
export function BackupRestore() { const [file, setFile] = useState<File>(), [payload, setPayload] = useState<unknown>(), [summary, setSummary] = useState<{
    tables: number;
    rows: number;
    warnings: string[];
}>(), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(); const seq = useRef(0); return <article className="settings-card import-card"><h2>Восстановить из бэкапа</h2><p>{backupRestoreHelp}</p><input className="settings-file-input" type="file" accept=".json,application/json" disabled={busy} onChange={async (e) => { const f = e.target.files?.[0], generation = ++seq.current; setFile(f); setPayload(undefined); setSummary(undefined); setError(undefined); if (!f)
    return; if (f.size > 25 * 1024 * 1024) {
    setError('Файл больше допустимых 25 МБ.');
    return;
} setBusy(true); try {
    const value: unknown = JSON.parse(await f.text()), s = await mutate<{
        tables: number;
        rows: number;
        warnings: string[];
    }>('/api/backup/validate', 'POST', value);
    if (generation === seq.current) {
        setPayload(value);
        setSummary(s);
    }
}
catch (err) {
    if (generation === seq.current)
        setError(err);
}
finally {
    if (generation === seq.current)
        setBusy(false);
} }}/>{summary && <div className="backup-preview show"><b>{file?.name}</b><p>Разделов данных: {summary.tables} · строк: {summary.rows}</p><p>{summary.warnings?.join(' ') || 'Файл проверен — можно восстановить данные.'}</p></div>}<div className="backup-warning">При восстановлении текущие данные из бэкапа будут заменены. Перед импортом рекомендуется скачать свежую резервную копию.</div><button className="settings-button restore" disabled={busy || !payload} onClick={async () => { if (!confirm('Восстановить данные из выбранного бэкапа? Текущие данные будут заменены.'))
    return; setBusy(true); try {
    await mutate('/api/backup', 'POST', payload);
    clearCache();
    location.reload();
}
catch (e) {
    setError(e);
    setBusy(false);
} }}>Восстановить данные</button><ErrorText error={error}/></article>; }
