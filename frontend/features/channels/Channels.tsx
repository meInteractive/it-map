import { useRef, useState } from 'react';
import { useDirty } from '../../hooks/useDirty';
import { useApi, mutate } from '../../api/client';
import { ActionForm, ErrorText, Field, State, Icon, safeUrl, dateLabel } from '../../components/Common';
import { Drawer } from '../../components/Drawer';
import type { Channel, Navigation, VacancyList } from '../../../shared/contracts';
import { employerName, publicationLabel } from '../vacancies/model';
export function ChannelCreateForm({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
    const [name, setName] = useState(''), [url, setUrl] = useState(''), [saving, setSaving] = useState(false);
    const dirty = Boolean(name || url); useDirty(dirty);
    function close() { if (!saving && (!dirty || confirm('Закрыть форму без сохранения изменений?'))) onClose(); }
    return <article className="channel-create-card"><h2>Новый Telegram-канал</h2>
        <ActionForm lockWhileBusy onBusyChange={setSaving} onSubmit={async () => {
            await mutate('/api/channels', 'POST', { name, url }); onSaved();
        }}>{busy => <>
            <div className="task-form-grid">
                <Field label="Название"><input className="task-input" required maxLength={256} value={name} onChange={e => setName(e.target.value)}/></Field>
                <Field label="Ссылка на канал"><input className="task-input" required type="url" maxLength={2048} value={url} onChange={e => setUrl(e.target.value)} placeholder="https://t.me/channel"/></Field>
            </div>
            <div className="task-form-actions"><button className="task-add" type="submit" disabled={busy}>Добавить канал</button>
                <button className="task-add channel-create-cancel" type="button" disabled={busy} onClick={close}>Отмена</button></div>
        </>}</ActionForm>
    </article>;
}
export function ChannelDeleteButton({ channel }: { channel: Channel }) {
    const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>();
    const inFlight = useRef(false);
    async function remove() {
        if (inFlight.current || !confirm('Удалить канал «' + channel.name + '»?')) return;
        inFlight.current = true; setBusy(true); setError(undefined);
        try { await mutate('/api/channels/' + channel.id, 'DELETE'); }
        catch (e) { setError(e); }
        finally { inFlight.current = false; setBusy(false); }
    }
    return <><button className="job-delete" type="button" title="Удалить канал"
        aria-label={'Удалить канал «' + channel.name + '»'} disabled={busy} onClick={remove}><Icon name="trash"/></button><ErrorText error={error}/></>;
}
function Comment({ channel, admin }: { channel: Channel; admin: boolean }) {
    const [value, setValue] = useState(channel.comment || '');
    return <ActionForm className="channel-comment-form" onSubmit={async () => {
        if (admin) await mutate('/api/channels/' + channel.id, 'PUT', { comment: value });
    }}>{busy => <>
        <label className="channel-comment-field"><span>Комментарий</span><textarea
            maxLength={512} disabled={!admin} value={value}
            placeholder="Служебная заметка о канале" onChange={e => setValue(e.target.value)}/></label>
        {admin && <div className="channel-comment-actions"><button className="crm-save"
            type="submit" disabled={busy}>Сохранить комментарий</button></div>}
    </>}</ActionForm>;
}
export function ChannelDetails({ channel }: { channel: Channel }) {
    return <div className="channel-details">
        <span>Ссылка</span><a className="channel-link" href={safeUrl(channel.url)} target="_blank" rel="noopener noreferrer">{channel.url}</a>
        <span>Username</span><span>{channel.username || '—'}</span>
        <span>Последняя проверка</span><span>{dateLabel(channel.lastSuccessfulCheckAt)}</span>
    </div>;
}
export function ChannelVacancies({ records, nav }: { records: VacancyList['vacancies']; nav: Navigation }) {
    return <div className="channel-vacancies">{records.length ? records.map(v => <article key={v.id} className="channel-vacancy">
        <button type="button" onClick={() => nav.openVacancy(v.id, 'channel')}>{v.title}</button>
        <small>{publicationLabel(v)}{employerName(v) ? ' · ' + employerName(v) : ''}</small>
    </article>) : <div className="vacancy-drawer-empty">Связанных вакансий пока нет.</div>}</div>;
}
export default function ChannelDrawer({ id, nav, admin, onClose }: {
    id: number;
    nav: Navigation;
    admin: boolean;
    onClose: () => void;
}) {
    const q = useApi<{ channels: Channel[] }>('/api/channels');
    const v = useApi<VacancyList>('/api/vacancies?channel=' + id);
    const c = q.data?.channels.find(c => c.id === id);
    return <Drawer className="vacancy-drawer channel-drawer" title={c?.name || 'Telegram-канал'}
        subtitle={<span className="channel-drawer-kicker">Telegram-канал</span>} onClose={onClose}>
        <div className="vacancy-drawer-body">
            <State loading={!q.data && q.loading} error={q.error}>{c && <>
                <ChannelDetails channel={c}/><Comment key={c.id} channel={c} admin={admin}/>
            </>}</State>
            <div className="channel-vacancies-heading"><h3>Связанные вакансии</h3>
                <span className="jobs-count">{v.data?.vacancies.length ?? c?.vacancyCount ?? 0}</span></div>
            <State loading={!v.data && v.loading} error={v.error}>
                <ChannelVacancies records={v.data?.vacancies || []} nav={nav}/>
            </State>
        </div>
    </Drawer>;
}
