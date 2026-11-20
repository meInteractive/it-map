import type { VacancyDetail } from '../../../shared/contracts';
import { safeUrl } from '../../components/Common';

export interface VacancySourceLink {
  key: string;
  href: string;
  label: string;
}

// Источники вакансий должны вести только на проверенные внешние HTTP(S) адреса.
export function httpVacancySource(value: unknown): string | undefined {
  const url = safeUrl(value);
  if (!url) return undefined;
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? url : undefined;
  } catch {
    return undefined;
  }
}

export function vacancySourceLinks(v: VacancyDetail): VacancySourceLink[] {
  const links: VacancySourceLink[] = [];
  const seen = new Set<string>();
  function add(value: unknown, label: string) {
    const href = httpVacancySource(value);
    if (!href) return;
    const key = new URL(href).href;
    if (seen.has(key)) return;
    seen.add(key);
    links.push({ key, href, label });
  }

  add(v.url, 'Открыть источник');
  for (const source of v.publicationSources || [])
    add(source.url, 'Дополнительный источник #' + source.candidateId);
  for (const source of v.telegramSources || [])
    add(source.messageUrl, 'Пост в ' + source.channelName);
  return links;
}
