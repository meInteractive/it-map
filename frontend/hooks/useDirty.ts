import { useEffect, useId } from 'react';
const dirtyForms = new Set<string>();
export function confirmNavigation() { return !dirtyForms.size || confirm('Есть несохранённые изменения. Продолжить без сохранения?'); }
export function useDirty(dirty: boolean) { const id = useId(); useEffect(() => { if (!dirty)
    return; dirtyForms.add(id); const listener = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; }; window.addEventListener('beforeunload', listener); return () => { dirtyForms.delete(id); window.removeEventListener('beforeunload', listener); }; }, [dirty, id]); }
