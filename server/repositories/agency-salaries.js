// SQL projection used inside the vacancy transaction. Never writes a JS catalogue snapshot.
// Catalogue writers persist normalized salary arrays; numeric JSON strings are also supported.
function normalizedBound(index) {
  const path = `'$.' || level.name || '[${index}]'`;
  const value = `json_extract(agency.value, ${path})`;
  const type = `json_type(agency.value, ${path})`;
  return `CASE WHEN ${type} IN ('integer','real','true','false')
      OR (${type} = 'text' AND json_valid(${value}) AND
          CASE WHEN json_valid(${value}) THEN json_type(${value}) IN ('integer','real') ELSE 0 END)
    THEN CASE WHEN CAST(${value} AS REAL) BETWEEN 0 AND 9007199254740991
      AND CAST(${value} AS REAL) = CAST(${value} AS INTEGER)
      THEN CAST(${value} AS INTEGER) ELSE NULL END ELSE NULL END`;
}

export function agencySalaryRangeStatements(db, initialCatalog) {
  // Seed only if absent; a concurrently updated existing catalogue must never be replaced.
  const seed = db.prepare('INSERT OR IGNORE INTO app_catalog(id,data_json) VALUES(1,?)')
    .bind(JSON.stringify(initialCatalog));
  const update = db.prepare(`WITH
    levels(name) AS (VALUES ('middle'),('senior'),('lead'),('head')),
    ranges AS MATERIALIZED (
      SELECT company_name, LOWER(level) AS level, MIN(salary_from) AS lo, MAX(salary_to) AS hi
      FROM company_vacancies WHERE entity_type = 'agency'
        AND LOWER(level) IN ('middle','senior','lead','head')
        AND (salary_from IS NOT NULL OR salary_to IS NOT NULL)
      GROUP BY company_name, LOWER(level)
    ),
    candidates AS MATERIALIZED (
      SELECT agency.key, agency.value AS record, level.name,
        json_extract(agency.value, '$.salaryRangeSource.' || level.name) AS source,
        ${normalizedBound(0)} AS oldFrom, ${normalizedBound(1)} AS oldTo, ranges.lo, ranges.hi
      FROM app_catalog catalogue, json_each(catalogue.data_json, '$.agencies') agency
      CROSS JOIN levels level LEFT JOIN ranges
        ON ranges.company_name = json_extract(agency.value, '$.name') AND ranges.level = level.name
      WHERE catalogue.id = 1
    ),
    patches AS MATERIALIZED (
      SELECT key, json_group_object(name, json_array(lo,hi)) AS salaryPatch,
        json_group_object(name, CASE WHEN COALESCE(lo,0) <> 0 OR COALESCE(hi,0) <> 0
          THEN 'vacancies' ELSE 'manual' END) AS sourcePatch,
        SUM(CASE WHEN oldFrom IS NOT lo OR oldTo IS NOT hi
          OR ((COALESCE(lo,0) <> 0 OR COALESCE(hi,0) <> 0) AND COALESCE(source,'manual') <> 'vacancies')
          THEN 1 ELSE 0 END) AS changed
      FROM candidates WHERE source = 'vacancies' OR (COALESCE(oldFrom,0) = 0 AND COALESCE(oldTo,0) = 0)
      GROUP BY key
    ),
    rebuilt AS MATERIALIZED (
      SELECT json_group_array(json(record)) AS agencies, SUM(changed) AS changed FROM (
        SELECT CASE WHEN patches.key IS NULL THEN agency.value ELSE
          json_set(json_patch(agency.value, json(patches.salaryPatch)), '$.salaryRangeSource',
            json_patch(CASE WHEN json_type(agency.value,'$.salaryRangeSource') = 'object'
              THEN json_extract(agency.value,'$.salaryRangeSource') ELSE '{}' END, json(patches.sourcePatch)))
          END AS record, COALESCE(patches.changed,0) AS changed
        FROM app_catalog catalogue, json_each(catalogue.data_json,'$.agencies') agency
        LEFT JOIN patches ON patches.key = agency.key WHERE catalogue.id = 1
        ORDER BY CAST(agency.key AS INTEGER)
      )
    )
    UPDATE app_catalog SET data_json = json_set(data_json, '$.agencies',
      json((SELECT agencies FROM rebuilt)), '$.updated', ?), updated_at = CURRENT_TIMESTAMP
    WHERE id = 1 AND COALESCE((SELECT changed FROM rebuilt),0) > 0`)
    .bind(new Date().toISOString().slice(0,10));
  return [seed, update];
}
