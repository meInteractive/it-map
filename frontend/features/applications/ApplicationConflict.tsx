import {useEffect, useState} from 'react';
import {ApiError, request} from '../../api/client';
import {ErrorText, Field} from '../../components/Common';
import {sourceLabels, type ApplicationRecord, type Vacancy} from '../../../shared/contracts';

const labels = {
    vacancyId: 'Вакансия', status: 'Статус отклика', comment: 'Комментарий',
    recruiterName: 'Имя рекрутера', recruiterPhone: 'Телефон', recruiterEmail: 'Email',
    recruiterContactType: 'Тип контакта', applicationSources: 'Где откликнулись',
};
type Key = keyof typeof labels;
export type ApplicationValues = Pick<ApplicationRecord, Key>;
type Choices = Partial<Record<Key, 'draft' | 'current'>>;
const keys = Object.keys(labels) as Key[];
function same(key: Key, a: ApplicationValues, b: ApplicationValues) {
    return key === 'applicationSources'
        ? JSON.stringify([...a[key]].sort()) === JSON.stringify([...b[key]].sort())
        : a[key] === b[key];
}
export function applicationConflicts(base: ApplicationValues, draft: ApplicationValues, current: ApplicationValues) {
    return keys.filter(key => !same(key, base, draft) && !same(key, base, current) && !same(key, draft, current));
}
export function reconcileApplication(base: ApplicationValues, draft: ApplicationValues, current: ApplicationRecord, choices: Choices = {}) {
    const conflicts = applicationConflicts(base, draft, current);
    if (conflicts.some(key => !choices[key])) throw new Error('Выберите значение каждого конфликтующего поля.');
    let merged = {...current};
    for (const key of keys) {
        if (!same(key, base, draft) && (!conflicts.includes(key) || choices[key] === 'draft')) {
            merged = {...merged, [key]: draft[key]};
        }
    }
    return merged;
}
export const isApplicationConflict = (error: unknown) => error instanceof ApiError && [409, 428].includes(error.status);

export function ApplicationConflict({base, draft, vacancies = [], onResolved}: {
    base: ApplicationRecord;
    draft: ApplicationValues;
    vacancies?: Vacancy[];
    onResolved: (current: ApplicationRecord, merged: ApplicationRecord) => void;
}) {
    const [current, setCurrent] = useState<ApplicationRecord>();
    const [choices, setChoices] = useState<Choices>({});
    const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
    useEffect(() => { setChoices({}); }, [JSON.stringify(draft)]);
    const conflicts = current ? applicationConflicts(base, draft, current) : [];
    function value(key: Key, record: ApplicationValues) {
        const item = record[key];
        if (key === 'applicationSources') return (item as string[]).map(source => sourceLabels[source] || source).join(', ') || 'Не указано';
        if (key === 'vacancyId') return item == null ? 'Без вакансии' : vacancies.find(v => v.id === item)?.title || (current?.vacancyId === item ? current.vacancyTitle : base.vacancyId === item ? base.vacancyTitle : '') || 'Вакансия';
        if (key === 'recruiterContactType') return item === 'direct' ? 'Прямой работодатель' : item === 'agency' ? 'Агентство' : 'Не указан';
        return String(item || 'Не указано');
    }
    return <div className="application-conflict" role="region" aria-label="Согласование изменений отклика">
        <p>Отклик изменился. Ваш черновик остаётся в форме.</p>
        <button type="button" disabled={busy} onClick={async () => {
            setBusy(true); setError(undefined);
            try {
                const result = await request<{record: ApplicationRecord}>('/api/applications/' + base.id);
                setCurrent(result.record); setChoices({});
            } catch (err) { setError(err); setCurrent(undefined); }
            finally { setBusy(false); }
        }}>{busy ? 'Загрузка…' : 'Загрузить актуальный отклик'}</button>
        <ErrorText error={error}/>
        {current && <>
            <p>Компания: {current.companyName}. Актуальный статус: <b>{current.status || 'Нет отклика'}</b>. Изменения других полей будут сохранены.</p>
            {conflicts.map(key => <Field key={key} label={labels[key]}>
                <p className="application-conflict-value">Ваша правка: {value(key, draft)}</p>
                <p className="application-conflict-value">Сохранено сейчас: {value(key, current)}</p>
                <select aria-label={'Согласовать: ' + labels[key]} value={choices[key] || ''} onChange={e => setChoices({...choices, [key]: e.target.value})}>
                    <option value="">Выберите значение</option><option value="draft">Оставить мою правку</option><option value="current">Взять сохранённое значение</option>
                </select>
            </Field>)}
            <p>Примените выбранные значения, проверьте форму и нажмите «Сохранить».</p>
            <button type="button" disabled={busy || conflicts.some(key => !choices[key])} onClick={() => onResolved(current, reconcileApplication(base, draft, current, choices))}>Применить к форме</button>
        </>}
    </div>;
}
