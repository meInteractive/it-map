import { Suspense, type ReactNode } from 'react';
import { Icon } from './Common';
import { Drawer } from './Drawer';

type PanelKind = 'company' | 'vacancy' | 'channel' | 'editor';

// Each lazy panel owns its loading state; opening a child must keep its parent visible.
export function PanelBoundary({ kind, title, onClose, children }: {
    kind: PanelKind;
    title: string;
    onClose: () => void;
    children: ReactNode;
}) {
    const loading = kind === 'company' ? <aside className="panel" role="dialog" aria-label={title} aria-busy="true">
        <div className="panel-top">
            <button className="close" onClick={onClose} aria-label="Закрыть карточку"><Icon name="x"/></button>
            <div className="eyebrow"><span className="rank">—</span></div>
            <h1>{title}</h1><div className="sector">Загрузка компании…</div>
        </div>
        <div className="content"><div className="empty" role="status">Загрузка…</div></div>
    </aside> : <Drawer title={title} onClose={onClose}
        className={'vacancy-drawer' + (kind === 'channel' ? ' channel-drawer' : kind === 'editor' ? ' vacancy-editor' : '')}>
        <div className="vacancy-drawer-body" aria-busy="true"><div className="empty" role="status">Загрузка…</div></div>
    </Drawer>;
    return <Suspense fallback={loading}>{children}</Suspense>;
}
