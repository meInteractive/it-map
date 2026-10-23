import { useState } from 'react';
import { useApi } from '../../api/client';
import { State } from '../../components/Common';
import { Markdown } from '../../components/Markdown';
interface Manifest {
    revision: number;
    updatedAt: string;
    versions: Record<string, string | number>;
    sections: {
        id: string;
        title: string;
        file: string;
    }[];
}
export default function Documentation() {
    const manifest = useApi<Manifest>('/api/docs');
    const [section, setSection] = useState('user');
    const doc = useApi<string>(manifest.data ? '/api/docs/' + section : null);
    return <State loading={!manifest.data && manifest.loading} error={manifest.error}>
        {manifest.data && <div className="docs-layout">
            <nav className="docs-nav" aria-label="Разделы документации">
                {manifest.data.sections.map(s => <button key={s.id}
                    aria-current={section === s.id ? 'page' : undefined}
                    onClick={() => setSection(s.id)}>{s.title}</button>)}
            </nav>
            <div className="docs-content">
                <div className="docs-toolbar">
                    <span className="docs-meta">Редакция {manifest.data.revision} · {manifest.data.updatedAt} · {Object.entries(manifest.data.versions).map(([k, v]) => k + ': ' + v).join(' · ')}</span>
                    <a className="settings-button secondary" href={'/api/docs/' + section + '?download=1'} download>Скачать Markdown</a>
                </div>
                <State loading={!doc.data && doc.loading} error={doc.error}>
                    <article className="docs-article" onClick={e => {
                        const a = (e.target as HTMLElement).closest('a');
                        if (!a) return;
                        const match = manifest.data?.sections.find(s => s.file === a.getAttribute('href')?.split('#')[0]);
                        if (match) {
                            e.preventDefault();
                            setSection(match.id);
                        }
                    }}><Markdown className="docs-markdown" value={doc.data || ''} empty="Документ пуст."/></article>
                </State>
            </div>
        </div>}
    </State>;
}
