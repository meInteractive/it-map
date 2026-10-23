export function companyRankLabel(value: unknown): string {
    const rank = Number(value);
    return Number.isInteger(rank) && rank > 0 ? '№ ' + rank : '';
}
