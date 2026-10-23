import { useState } from 'react';
import { useApi, mutate } from '../../api/client';
import { ActionForm, Icon, State, ErrorText } from '../../components/Common';
interface Contact {
    id?: number;
    position: number;
    fullName: string;
    linkedinUrl: string;
    facebookUrl: string;
    setkaUrl: string;
    telegram: string;
    phone: string;
    email: string;
    specialization: string;
}
const blank: Contact = { position: 1, fullName: '', linkedinUrl: '', facebookUrl: '', setkaUrl: '', telegram: '', phone: '', email: '', specialization: '' };
const fields: [
    keyof Omit<Contact, 'id' | 'position'>,
    string,
    number,
    string
][] = [['fullName', 'ФИО', 256, 'text'], ['linkedinUrl', 'LinkedIn', 2048, 'url'], ['facebookUrl', 'Facebook', 2048, 'url'], ['setkaUrl', 'Сетка', 2048, 'url'], ['telegram', 'Telegram', 128, 'text'], ['phone', 'Номер телефона', 64, 'tel'], ['email', 'Email', 254, 'email'], ['specialization', 'Специализация контакта', 512, 'text']];
function externalContactUrl(value: string) {
    try {
        const url = new URL(String(value || '').trim());
        return ['https:', 'http:'].includes(url.protocol) ? url.href : undefined;
    } catch { return undefined; }
}
export function ContactLinks({ contact }: { contact: Contact }) {
    const links: { label: string; href: string }[] = [];
    const add = (label: string, href: string | undefined) => {
        if (label && href) links.push({ label, href });
    };
    add('LinkedIn', externalContactUrl(contact.linkedinUrl));
    add('Facebook', externalContactUrl(contact.facebookUrl));
    add('Сетка', externalContactUrl(contact.setkaUrl));
    const telegram = String(contact.telegram || '').trim();
    const username = telegram.replace(/^@/, '');
    add('Telegram: ' + telegram, externalContactUrl(telegram)
        || (/^[A-Za-z0-9_]{3,64}$/.test(username) ? 'https://t.me/' + username : undefined));
    const phone = String(contact.phone || '').trim(), phoneHref = phone.replace(/[^\d+]/g, '');
    add(phone, phone && phoneHref ? 'tel:' + phoneHref : undefined);
    const email = String(contact.email || '').trim();
    add(email, email ? 'mailto:' + email : undefined);
    return links.length ? <div className="hiring-contact-links">{links.map(({ label, href }, i) => {
        const external = /^https?:/i.test(href);
        return <a className="hiring-contact-link" key={i} href={href}
            target={external ? '_blank' : undefined} rel={external ? 'noopener noreferrer' : undefined}>{label}</a>;
    })}</div> : <div className="hiring-contact-no-links">Способы связи пока не указаны.</div>;
}
export default function Contacts({ companyName, admin }: {
    companyName: string;
    admin: boolean;
}) { const q = useApi<{
    contacts: Contact[];
}>('/api/hiring-contacts?company=' + encodeURIComponent(companyName)), [editor, setEditor] = useState<Contact | null>(null), [error, setError] = useState<unknown>(); const records = q.data?.contacts || []; return <div className="hiring-contacts-section"><h2>Ответственные за найм</h2><State loading={!q.data && q.loading} error={q.error}>{admin && <div className="hiring-contacts-toolbar"><span className="hiring-contacts-count">{records.length} из 3</span>{records.length < 3 && <button className="hiring-contact-add" onClick={() => setEditor({ ...blank, position: [1, 2, 3].find(p => !records.some(r => r.position === p)) || 1 })}><Icon name="plus"/>Добавить контакт</button>}</div>}<div className="hiring-contacts-list">{records.map(c => <article className="hiring-contact" key={c.id}><div className="hiring-contact-head"><div className="hiring-contact-name">{c.fullName}</div><div className="hiring-contact-head-actions"><span className="hiring-contact-position">Контакт {c.position}</span>{admin && <><button className="hiring-contact-action" type="button" aria-label={'Редактировать контакт ' + c.fullName} title="Редактировать" onClick={() => setEditor(c)}><Icon name="pencil"/></button><button className="hiring-contact-action delete" type="button" aria-label={'Удалить контакт ' + c.fullName} title="Удалить" onClick={async () => { if (!confirm('Удалить контакт «' + c.fullName + '»?'))
    return; try {
    await mutate('/api/hiring-contacts/' + c.id, 'DELETE');
}
catch (e) {
    setError(e);
} }}><Icon name="trash"/></button></>}</div></div>{c.specialization && <div className="hiring-contact-specialization">{c.specialization}</div>}<ContactLinks contact={c}/></article>)}</div>{!records.length && <div className="hiring-contacts-empty">Контакты ответственных за найм пока не добавлены.</div>}<ErrorText error={error}/>{editor && <ActionForm key={editor.id || 'new'} className="hiring-contact-form" onSubmit={async () => { await mutate('/api/hiring-contacts' + (editor.id ? '/' + editor.id : ''), editor.id ? 'PUT' : 'POST', { ...editor, companyName }); setEditor(null); }}>{busy => <><h3>{editor.id ? 'Редактировать контакт' : 'Новый контакт'} · позиция {editor.position}</h3><div className="hiring-contact-form-grid">{fields.map(([k, label, max, type]) => <label key={k} className={'hiring-contact-field' + (['fullName', 'specialization'].includes(k) ? ' full' : '')}><span>{label}</span><input required={k === 'fullName'} maxLength={max} type={type} value={editor[k]} onChange={e => setEditor({ ...editor, [k]: e.target.value })}/></label>)}</div><div className="hiring-contact-form-actions"><button type="submit" className="hiring-contact-save" disabled={busy}>Сохранить</button><button type="button" className="hiring-contact-cancel" onClick={() => setEditor(null)}>Отмена</button></div></>}</ActionForm>}</State></div>; }
