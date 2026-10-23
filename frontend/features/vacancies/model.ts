import {publicationDate,effectivePublicationDate} from '../../../shared/publication-period';
export {publicationDate} from '../../../shared/publication-period';
import type { Entity, EntityType, Vacancy, VacancyRole } from '../../../shared/contracts';
import { matchesEntity, type Filters } from '../companies/Filters';
export interface VacancyFilters {
    levels: string[];
    roleIds: number[];
    favoriteOnly: boolean;
    showHidden: boolean;
}
export const defaultVacancyFilters: VacancyFilters = { levels: [], roleIds: [], favoriteOnly: false, showHidden: false };
export function displayEntity(v: Vacancy, view: EntityType) { return view === 'channel' ? v.telegramSources?.[0]?.channelName || '' : v.entityType === 'agency' ? (view === 'agency' ? v.companyName : v.realCompanyName) : view === 'company' && v.entityType !== 'channel' ? v.companyName : ''; }
export function vacancyContext(v: Vacancy, view: EntityType) {
    const name = displayEntity(v, view);
    const channelId = view === 'channel' ? v.telegramSources?.[0]?.channelId : undefined;
    return { name: name || v.companyName, type: name ? view : v.entityType, channelId };
}
export function employerName(v: Vacancy) { return v.entityType === 'company' ? v.companyName : v.realCompanyName; }
export function isMisassigned(v: Vacancy) { return v.entityType === 'company' && v.companyName === 'ЮMoney' && [262, 263, 264, 265, 266].includes(v.id); }
export function visibleVacancy(v: Vacancy, type: EntityType, entities: Map<string, Entity>, filters: Filters, vf: VacancyFilters, query: string, roleTree: VacancyRole[] = []) { const entity = entities.get(displayEntity(v, type)); const scope = type === 'channel' ? !!v.telegramSources?.length : type === 'company' && isMisassigned(v) || !!entity && (type === 'agency' ? entity.entityType === 'agency' : entity.entityType !== 'agency' && v.entityType !== 'channel') && matchesEntity(entity, filters); const acceptedRoleIds=new Set(vf.roleIds.flatMap(id=>{const role=roleTree.find(item=>item.id===id);return [id,...(role?.descendantIds||[])];})); const roleMatch=!vf.roleIds.length||(v.roles||[]).some(role=>acceptedRoleIds.has(role.id)); return scope && (!v.hidden || vf.showHidden) && (!vf.favoriteOnly || v.favorite) && (!vf.levels.length || vf.levels.includes(v.level)) && roleMatch && v.title.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()); }
export function vacancyDateLabel(value: string | undefined | null) {
    const date = publicationDate(value);
    return date ? String(date.day).padStart(2, '0') + '.' + String(date.month).padStart(2, '0') + '.' + date.year : '—';
}
export function publicationLabel(v: Vacancy) {
    return publicationDate(v.publishedAt) ? vacancyDateLabel(v.publishedAt)
        : effectivePublicationDate(v) ? '~ ' + vacancyDateLabel(publicationDate(v.loadedAt)?v.loadedAt:v.createdAt) : '—';
}
export function comparePublication(a: Vacancy, b: Vacancy) { const at = effectivePublicationDate(a)?.time ?? -Infinity, bt = effectivePublicationDate(b)?.time ?? -Infinity; return (bt !== at ? bt - at : 0) || String(b.createdAt || '').localeCompare(String(a.createdAt || '')) || b.id - a.id; }
export function compareVacancies(a: Vacancy, b: Vacancy, sort: string, type: EntityType, general: Set<string>) {
    if (sort === 'relevance') {
        const value = (v: Vacancy) => Number.isFinite(Number(v.relevance)) ? Number(v.relevance) : -Infinity;
        const diff = value(b) - value(a);
        if (diff)
            return diff;
    }
    if (sort === 'application') {
        const diff = Number(!!b.applicationId || general.has(b.companyName)) - Number(!!a.applicationId || general.has(a.companyName));
        if (diff)
            return diff;
    }
    return comparePublication(a, b) || (sort === 'relevance' ? a.companyName.localeCompare(b.companyName, 'ru') : displayEntity(a, type).localeCompare(displayEntity(b, type), 'ru'));
}
export function vacancySalary(v: Vacancy, entities: Entity[], view: EntityType = 'company') {
    const format = (n: number) => n.toLocaleString('ru-RU');
    if (v.salaryFrom && v.salaryTo)
        return { value: format(v.salaryFrom) + ' — ' + format(v.salaryTo) + ' ₽', estimated: false };
    if (v.salaryFrom)
        return { value: 'от ' + format(v.salaryFrom) + ' ₽', estimated: false };
    if (v.salaryTo)
        return { value: 'до ' + format(v.salaryTo) + ' ₽', estimated: false };
    const e = entities.find(e => e.name === displayEntity(v, view)), range = e?.[v.level?.trim().toLowerCase()];
    const k = (n: unknown) => Math.round(Number(n) / 1000) + 'к';
    if (Array.isArray(range) && range.some(n => Number(n) > 0))
        return { value: '~ ' + (range[0] > 0 && range[1] > 0 ? k(range[0]) + ' - ' + k(range[1]) : range[0] > 0 ? 'от ' + k(range[0]) : 'до ' + k(range[1])), estimated: true };
    return { value: 'зарплата не указана', estimated: false };
}
