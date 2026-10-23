export function hhVacancyId(value){try{const u=new URL(value);return u.protocol==='https:'&&/(^|\.)hh\.ru$/.test(u.hostname)&&/^\/vacancy\/\d+\/?$/.test(u.pathname)&&!u.username&&!u.password&&!u.port?u.pathname.match(/^\/vacancy\/(\d+)\/?$/)[1]:null;}catch{return null;}}
export const isHhVacancyUrl=value=>hhVacancyId(value)!==null;
const normalized=value=>String(value||'').normalize('NFKC').toLocaleLowerCase('ru').replace(/\[([^\]]+)\]\(https?:\/\/[^\s)]+\)/g,'$1').replace(/https?:\/\/[^\s<>]+/g,' ').replace(/[*_`#>]+/g,' ').replace(/\s+/g,' ').trim();
// A title alone is never identity. Exact normalized substantive descriptions,
// same employer and a cross-source HH pair are required; ambiguous matches wait.
export function sameHhPublication(main,candidate,data,companyName,entityType='company'){
 const body=normalized(data.description_md);
 return main.company_name===companyName&&main.entity_type===entityType&&isHhVacancyUrl(main.url)!==isHhVacancyUrl(candidate.vacancyUrl)&&normalized(main.title)===normalized(data.title)&&body.length>=250&&normalized(main.description_md)===body;
}
export const PROTECT_PARSER_URL_SQL=`CREATE TRIGGER IF NOT EXISTS parser_protect_url AFTER UPDATE OF url ON company_vacancies WHEN OLD.url IS NOT NEW.url AND NOT EXISTS(SELECT 1 FROM vacancy_import_write_context WHERE vacancy_id=NEW.id) BEGIN UPDATE vacancy_parser_fields SET manual=1 WHERE vacancy_id=NEW.id AND field='url'; END`;
// Only a URL already recorded by the parser is eligible for legacy ownership.
export const BACKFILL_PARSER_URL_SQL=`INSERT OR IGNORE INTO vacancy_parser_fields(vacancy_id,field,value_json,candidate_id)
 SELECT v.id,'url',json_quote(v.url),min(s.candidate_id) FROM company_vacancies v JOIN vacancy_publication_sources s ON s.vacancy_id=v.id AND s.url=v.url JOIN vacancy_candidates c ON c.id=s.candidate_id AND c.main_vacancy_id=v.id GROUP BY v.id`;
