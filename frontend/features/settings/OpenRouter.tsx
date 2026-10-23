import { useState } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, ErrorText, State, dateLabel } from '../../components/Common';
interface Model {
    id: string;
    name: string;
    rank: number;
    intelligence_index: number | null;
    agentic_index: number | null;
    coding_index: number | null;
    context_length: number;
    letter_timing?: {
        success_count: number;
        failure_count: number;
        average_ms: number | null;
        last_ms: number;
        last_failure_ms: number;
    };
}
interface RouterState {
    configured: boolean;
    key_mask: string;
    connection_status: string;
    primary_model_id: string;
    active_test_model_id: string;
    last_used_model: string;
    last_used_at: string;
    models_synced_at: string;
    models: Model[];
    warning?: string;
    empty_message?: string;
}
export default function OpenRouter() { const q = useApi<RouterState>('/api/openrouter'), [key, setKey] = useState(''), [speed, setSpeed] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(), [message, setMessage] = useState(''); const state = q.data; async function action(name: string) { setBusy(true); setError(undefined); setMessage(name === 'test' ? 'Проверяем AI-запрос. Модель может отвечать до 210 секунд…' : 'Загрузка…'); try {
    const r = await mutate<{
        model?: string;
        attempted_models?: string[];
        fallback_used?: boolean;
    }>('/api/openrouter/' + name, 'POST');
    setMessage(name === 'test' ? 'Тест выполнен. Ответила модель ' + r.model + '. Проверены по порядку: ' + (r.attempted_models || []).join(' → ') + '.' : name === 'refresh' ? 'Список моделей обновлён.' : 'Подключение проверено.');
}
catch (e) {
    setError(e);
    setMessage('');
    q.reload();
}
finally {
    setBusy(false);
} } const models = state?.models ? [...state.models].sort((a, b) => speed ? (a.letter_timing?.average_ms ?? Infinity) - (b.letter_timing?.average_ms ?? Infinity) || a.rank - b.rank : a.rank - b.rank) : [];
    return <div className="or-panel">
        <div className="or-heading">
            <div><h2>OpenRouter</h2><p>Бесплатные рассуждающие модели. При ошибке используется следующая модель рейтинга.</p></div>
            {state && !q.error && <span className={'or-status ' + state.connection_status}>
                {{ missing: 'Ключ не указан', unchecked: 'Ключ сохранён · не проверен', connected: 'Подключено', error: 'Ошибка подключения' }[state.connection_status] || 'Не проверено'}
            </span>}
        </div>
        <ActionForm className="or-card" onSubmit={async () => {
            await mutate('/api/openrouter/key', 'PUT', { apiKey: key });
            setKey('');
            setMessage('Ключ сохранён.');
        }}>{saving => <>
            <label htmlFor="openrouterKey">API-ключ OpenRouter</label>
            <div className="or-key-row">
                <input id="openrouterKey" type="password" autoComplete="off" required value={key}
                    placeholder={state?.configured ? 'Сохранён ключ ' + state.key_mask + ' · введите новый для замены' : 'sk-or-…'}
                    onChange={e => setKey(e.target.value)}/>
                <button type="submit" className="settings-button" disabled={saving || busy}>Сохранить ключ</button>
                <button type="button" className="settings-button secondary" disabled={busy}
                    onClick={() => void action('check')}>Проверить подключение</button>
            </div>
            <p className="or-help">Ключ хранится в зашифрованном виде. После сохранения видны только последние 4 символа.</p>
            <div className="or-message" role="status">{message}</div>
            <ErrorText error={error}/>
        </>}</ActionForm>
        {state && !q.error && <div className="or-meta">
            <div><span>Первая в рейтинге</span><strong>{state.primary_model_id || 'Пока не выбрана'}</strong><small>Начало тестового AI-запроса</small></div>
            <div><span>Активная для писем</span><strong>{state.active_test_model_id || state.primary_model_id || 'Пока не выбрана'}</strong><small>Последняя модель, успешно ответившая на тест</small></div>
            <div><span>Последняя фактически использованная</span><strong>{state.last_used_model || 'Пока не выбрана'}</strong><small>Последний запрос: {dateLabel(state.last_used_at)}</small></div>
        </div>}
        <div className="or-table-heading">
            <div><h3>Рейтинг подходящих моделей</h3><p>Контекст от 256K · поддержка рассуждений · бесплатный ввод и вывод</p></div>
            <div className="or-actions">
                <button type="button" className="settings-button secondary" disabled={busy}
                    onClick={() => void action('refresh')}>Обновить модели</button>
                <button type="button" className="settings-button secondary" disabled={busy}
                    onClick={() => void action('test')}>Тестовый запрос</button>
            </div>
        </div>
        <State loading={!state && q.loading} error={q.error}>{state && <>
            <p className="or-help">Список обновлён: {dateLabel(state.models_synced_at)} · автоматическое обновление при обращении через 6 часов</p>
            <ErrorText error={state.warning}/>
<div className="or-table-wrap"><table className="or-table"><thead><tr>
    <th scope="col">№</th><th scope="col">Модель</th><th scope="col">Статус</th>
    <th scope="col" title="Общая интеллектуальная способность по Artificial Analysis. Главный критерий рейтинга.">Intelligence ⓘ</th>
    <th scope="col" title="Выполнение многошаговых агентных задач. Второй критерий при равном Intelligence.">Agentic ⓘ</th>
    <th scope="col" title="Способности к программированию. Третий критерий рейтинга.">Coding ⓘ</th>
    <th scope="col" title="Максимальное окно контекста в токенах. Точное значение — при наведении.">Контекст ⓘ</th>
    <th scope="col">Рассуждения</th><th scope="col">Стоимость</th>
    <th scope="col" title="Среднее и последнее время успешной генерации письма; не учитывает время модели до получения ответа в неудачных запросах.">
        <button type="button" className="or-sort-button" aria-pressed={speed}
            title="Сортировать модели по среднему времени успешной генерации"
            onClick={() => setSpeed(!speed)}>Время письма ↕</button>
    </th>
</tr></thead><tbody>{models.map(m => <tr key={m.id}><td>{m.rank}</td><td><strong>{m.name}</strong><code>{m.id}</code></td><td><span className="or-badge">{m.rank === 1 ? 'Основная' : 'Резервная'}</span></td>{[m.intelligence_index, m.agentic_index, m.coding_index].map((s, i) => <td key={i}>{s == null ? 'нет данных' : s.toLocaleString('ru-RU', { maximumFractionDigits: 2 })}</td>)}<td title={m.context_length + ' токенов'}>{(m.context_length / 1024).toLocaleString('ru-RU', { maximumFractionDigits: 1 })}K</td><td>Да</td><td>Бесплатно</td><td>{m.letter_timing?.success_count ? <><strong>{Math.round((m.letter_timing.average_ms || 0) / 1000)} с</strong><small>последнее {Math.round(m.letter_timing.last_ms / 1000)} с · {m.letter_timing.success_count} ответов · {m.letter_timing.failure_count || 0} неудач</small></> : m.letter_timing?.failure_count ? <>Нет успешных ответов<small>{m.letter_timing.failure_count} неудач · последняя {Math.round(m.letter_timing.last_failure_ms / 1000)} с</small></> : 'нет замеров'}</td></tr>)}{!models.length && <tr><td colSpan={10} className="or-empty">{state.empty_message || 'Сохраните API-ключ, чтобы загрузить подходящие модели.'}</td></tr>}</tbody></table></div>
        </>}</State>
    </div>;
}
