// В URL хранится только разрешённый раздел, без данных пользователя или карточки.
export const knownSections = ['tasks', 'applications', 'vacancies', 'list', 'map', 'settings'] as const;
export type Section = typeof knownSections[number];
export function sectionFromPath(path: string): Section {
    const match = /^\/(tasks|applications|vacancies|list|map|settings)\/?$/.exec(path);
    return (match?.[1] as Section | undefined) || 'tasks';
}
export function sectionPath(section: Section): string {
    return section === 'tasks' ? '/' : '/' + section;
}
export function rememberLoginDestination(path: string, storage: Pick<Storage, 'setItem'>) {
    const section = sectionFromPath(path);
    if (section !== 'tasks') storage.setItem('itmap:post-login-section', section);
}
export function restoreLoginDestination(path: string, storage: Pick<Storage, 'getItem' | 'removeItem'>): Section {
    const section = sectionFromPath(path);
    if (path !== '/') return section;
    const requested = storage.getItem('itmap:post-login-section');
    if (!requested) return section;
    storage.removeItem('itmap:post-login-section');
    return knownSections.some(known => known === requested) ? requested as Section : section;
}
