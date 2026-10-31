import type {Entity} from '../../../shared/contracts';

// Одна нормализация для поиска по каталогу и для маркеров карты.
export function normalizeCompanySearch(value: string): string {
    return value.trim().toLocaleLowerCase('ru-RU');
}

export function companyMatchesSearch(name: string, query: string): boolean {
    return normalizeCompanySearch(name).includes(normalizeCompanySearch(query));
}

export function searchCompanies<T extends Pick<Entity, 'name'>>(entities: T[], query: string): T[] {
    const term = normalizeCompanySearch(query);
    return term ? entities.filter(entity => normalizeCompanySearch(entity.name).includes(term)) : entities;
}
