import { useEffect, useRef, type ReactNode } from 'react';
import { Icon } from './Common';
export function Drawer({ title, subtitle, children, onClose, className = 'vacancy-drawer', headerExtra }: {
    title: string;
    subtitle?: ReactNode;
    children: ReactNode;
    onClose: () => void;
    className?: string;
    headerExtra?: ReactNode;
}) { const ref = useRef<HTMLElement>(null), closeRef = useRef(onClose); closeRef.current = onClose; useEffect(() => { const previous = document.activeElement as HTMLElement | null; ref.current?.querySelector<HTMLButtonElement>('button')?.focus(); const listener = (e: KeyboardEvent) => { if (e.key === 'Escape') {
    e.preventDefault();
    e.stopImmediatePropagation();
    closeRef.current();
} if (e.key === 'Tab') {
    const items = Array.from(ref.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled),select:not(:disabled),textarea:not(:disabled),[tabindex="0"]') || []).filter(e => e.getClientRects().length);
    if (!items.length)
        return;
    if (e.shiftKey && document.activeElement === items[0]) {
        e.preventDefault();
        items.at(-1)?.focus();
    }
    if (!e.shiftKey && document.activeElement === items.at(-1)) {
        e.preventDefault();
        items[0]?.focus();
    }
} }; ref.current?.addEventListener('keydown', listener); const el = ref.current; return () => { el?.removeEventListener('keydown', listener); previous?.focus({ preventScroll: true }); }; }, []); return <aside ref={ref} className={className} role="dialog" aria-label={title}><div className="vacancy-drawer-head"><div className="vacancy-drawer-topline"><span className="vacancy-drawer-company">{subtitle}</span><button className="vacancy-drawer-close" onClick={onClose} aria-label="Закрыть"><Icon name="x"/></button></div><h2>{title}</h2>{headerExtra}</div>{children}</aside>; }
