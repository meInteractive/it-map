import type { Entity } from '../../../shared/contracts';

export function mapCompanyLabel(company: Pick<Entity, 'name' | 'rank'>): string {
    const rank = Number(company.rank);
    const prefix = Number.isInteger(rank) && rank > 0 ? '№ ' + rank : '';
    return [prefix, company.name].filter(Boolean).join(' ');
}
