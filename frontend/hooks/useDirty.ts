import { useEffect, useId } from 'react';
const dirtyForms = new Set<string>();
let sessionExit = false;
export function allowSessionExit() {
  sessionExit = true;
}
export function confirmNavigation() {
  return !dirtyForms.size || confirm('Есть несохранённые изменения. Продолжить без сохранения?');
}
export function useDirty(dirty: boolean) {
  const id = useId();
  useEffect(() => {
    if (!dirty) return;
    dirtyForms.add(id);
    const listener = (e: BeforeUnloadEvent) => {
      if (sessionExit) return;
      e.preventDefault();
      e.returnValue = '';
    };
    // Нужен и в браузере, и в синтетических React-тестах без Window.
    if (typeof window !== 'undefined') window.addEventListener('beforeunload', listener);
    return () => {
      dirtyForms.delete(id);
      if (typeof window !== 'undefined') window.removeEventListener('beforeunload', listener);
    };
  }, [dirty, id]);
}
