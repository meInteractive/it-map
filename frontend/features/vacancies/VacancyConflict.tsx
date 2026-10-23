import {useRef, useState} from 'react';
import {request} from '../../api/client';
import {ErrorText, Field, KV} from '../../components/Common';
import type {VacancyDetail} from '../../../shared/contracts';

// Reading the latest projection must never rebase a draft or its letter scope.
// The effective GET letter may hide an existing personal letter under shared text.
export function VacancyConflict({id, disabled}: {id: number; disabled: boolean}) {
    const [current, setCurrent] = useState<VacancyDetail>();
    const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
    const inFlight = useRef(false);
    return <div className="application-conflict" role="region" aria-label="Конфликт изменений вакансии">
        <p>Вакансия изменилась. Ваш черновик остаётся в форме. Ниже можно посмотреть сохранённую версию; она не заменит ваши правки.</p>
        <button type="button" disabled={disabled || busy} onClick={async () => {
            if (disabled || inFlight.current) return;
            inFlight.current = true; setBusy(true); setError(undefined); setCurrent(undefined);
            try {
                // Bypass the memory cache: this is an explicit conflict check.
                const result = await request<{vacancy: VacancyDetail}>('/api/vacancies/' + id);
                if (!result.vacancy || result.vacancy.id !== id) throw new Error('Не удалось прочитать актуальную вакансию.');
                setCurrent(result.vacancy);
            } catch (err) { setError(err); }
            finally { inFlight.current = false; setBusy(false); }
        }}>{busy ? 'Загрузка актуальной версии…' : 'Показать актуальную версию'}</button>
        <ErrorText error={error}/>
        {current && <div role="region" aria-label="Актуальная версия вакансии">
            <p>Только для чтения. Для дальнейшего редактирования сохраните нужный текст черновика, закройте эту форму и откройте актуальную карточку заново. Закрытие с правками требует подтверждения.</p>
            <KV rows={[
                ['Владелец', current.companyName],
                ['Тип владельца', {company: 'Компания', agency: 'Агентство', channel: 'Канал'}[current.entityType]],
                ['Реальный работодатель', current.realCompanyName],
                ['Название', current.title], ['Ссылка', current.url],
                ['Роль', current.role], ['Уровень', current.level],
                ['Зарплата от', current.salaryFrom == null ? '—' : String(current.salaryFrom)],
                ['Зарплата до', current.salaryTo == null ? '—' : String(current.salaryTo)],
                ['Моя релевантность, %', current.relevance == null ? '—' : String(current.relevance)],
                ['Дата публикации', current.publishedAt], ['Дата загрузки', current.loadedAt],
                ['Обновлено', current.updatedAt],
                ['Письмо', current.coverLetterScope === 'personal' ? 'Личное, видно только вам' : 'Общее, видно всем'],
                ['Тема письма', current.coverLetterSubject],
            ]}/>
            <Field label="Сохранённый полный текст — Markdown"><textarea aria-label="Сохранённый полный текст" readOnly rows={10} value={current.descriptionMd}/></Field>
            <Field label="Сохранённое письмо — Markdown"><textarea aria-label="Сохранённое письмо" readOnly rows={6} value={current.coverLetterMd}/></Field>
            <h4>Сохранённые Telegram-источники</h4>
            {current.telegramSources.length ? current.telegramSources.map(source => <KV key={source.channelId} rows={[
                ['Канал', source.channelName], ['Ссылка на сообщение', source.messageUrl], ['Контакт', source.telegramContact],
            ]}/>) : <p>Нет источников.</p>}
        </div>}
    </div>;
}
