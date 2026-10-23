import {useEffect, useRef, useState} from 'react';
import {Icon} from './Common';
import {settingsSections} from '../features/settings/sections';

export function SettingsNavigation({admin, active, tab, onSelect}: {
  admin: boolean; active: boolean; tab: string; onSelect: (tab: string) => boolean;
}) {
  const [open, setOpen] = useState(false);
  const container = useRef<HTMLDivElement>(null), trigger = useRef<HTMLButtonElement>(null);
  const focusIndex = useRef<number | null>(null);
  const pointerType = useRef('');
  const items = settingsSections(admin);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => {
      if (!container.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    if (focusIndex.current !== null) {
      container.current?.querySelectorAll<HTMLButtonElement>('.settings-nav-item')[focusIndex.current]?.focus();
      focusIndex.current = null;
    }
    return () => document.removeEventListener('pointerdown', outside);
  }, [open]);
  return <div className="settings-nav" ref={container}
    onPointerEnter={event => {if (event.pointerType === 'mouse') setOpen(true);}}
    onPointerLeave={event => {if (event.pointerType === 'mouse') setOpen(false);}}
    onBlur={event => {if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOpen(false);}}
    onKeyDown={event => {
      if (event.key === 'Escape' && open) {event.preventDefault(); setOpen(false); trigger.current?.focus();}
    }}>
    <button ref={trigger} type="button" className="app-nav-button settings-nav-trigger"
      aria-current={active ? 'page' : undefined} aria-expanded={open} aria-controls="settings-navigation"
      onPointerDown={event => {pointerType.current = event.pointerType;}}
      onClick={event => {
        if (event?.detail && pointerType.current === 'mouse') setOpen(true);
        else setOpen(value => !value);
      }} onKeyDown={event => {
        if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
          event.preventDefault(); const index = event.key === 'ArrowUp' ? items.length - 1 : 0;
          if (open) container.current?.querySelectorAll<HTMLButtonElement>('.settings-nav-item')[index]?.focus();
          else {focusIndex.current = index; setOpen(true);}
        }
      }}><span className="app-nav-icon"><Icon name="settings"/></span><span>Настройки</span></button>
    {open && <div className="settings-nav-popover"><div id="settings-navigation" className="settings-nav-menu" aria-label="Подразделы настроек"
      onKeyDown={event => {
        if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
        event.preventDefault();
        const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button'));
        const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1
          : (current + (event.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length;
        buttons[next]?.focus();
      }}>{items.map(([key, label]) => <button key={key} type="button" className="settings-nav-item"
        aria-current={active && tab === key ? 'page' : undefined} onClick={() => {
          if (onSelect(key)) {setOpen(false); trigger.current?.focus();}
        }}>{label}</button>)}</div></div>}
  </div>;
}
