#!/usr/bin/env python3
"""Reject changes that would make the disposable SQL/API smoke seed unsafe."""
import json
import re
from pathlib import Path

seed_path = Path(__file__).resolve().parents[1] / 'db/testdata/smoke-catalog.sql'
lines = [line.strip() for line in seed_path.read_text(encoding='utf-8').splitlines()
         if line.strip() and not line.lstrip().startswith('--')]
expected_first = "INSERT INTO `company_crm` (`company_name`) VALUES ('CI Synthetic Employer');"
expected_second = "INSERT INTO `company_identity` (`id`, `company_name`) VALUES (1, 'CI Synthetic Employer');"
if lines[:2] != [expected_first, expected_second] or len(lines) != 3:
    raise SystemExit('FAIL: CI smoke seed must only contain its three approved synthetic inserts')
match = re.fullmatch(
    r"INSERT INTO `app_catalog` \(`id`, `data_json`\) VALUES \(1, '([^']+)'\);",
    lines[2],
)
if match is None:
    raise SystemExit('FAIL: unexpected SQL in synthetic catalog seed')
payload = json.loads(match.group(1))
if payload != {
    'companies': [{'name': 'CI Synthetic Employer', 'sector': 'CI test',
                   'offices': [], 'jobs': [], 'vacancySources': {}}],
    'agencies': [],
}:
    raise SystemExit('FAIL: catalog smoke seed has non-synthetic or unexpected records')
print('PASS: only three approved synthetic CI catalog rows; no private data')

# The read-model fixture is separate so it can never precede schema-only tests.
# Keep an exact allowlist: no arbitrary row contents, extra DML, or real URLs.
read_seed_path = seed_path.with_name('read-models.sql')
read_lines = [line.strip() for line in read_seed_path.read_text(encoding='utf-8').splitlines()
              if line.strip() and not line.lstrip().startswith('--')]
approved_read_rows = [
    '''INSERT INTO `company_vacancies` (`id`, `company_name`, `url`, `title`) VALUES (1, 'CI Synthetic Employer', 'https://example.invalid/ci-vacancy', 'CI Synthetic Vacancy');''',
    '''INSERT INTO `company_applications` (`id`, `company_name`, `vacancy_id`, `user_name`, `application_sources`) VALUES (1, 'CI Synthetic Employer', 1, 'Eugene', '["site"]');''',
    '''INSERT INTO `company_application_status_history` (`id`, `application_id`, `from_status`, `to_status`) VALUES (1, 1, '', 'Откликнулся');''',
    '''INSERT INTO `company_tasks` (`id`, `company_name`, `vacancy_id`, `user_name`, `type`, `note`, `due_at`) VALUES (1, 'CI Synthetic Employer', 1, 'Eugene', 'CI synthetic task', 'CI synthetic note', '2026-01-01T12:00:00.000Z');''',
]
if read_lines != approved_read_rows:
    raise SystemExit('FAIL: read-model seed must only contain its four approved synthetic inserts')
print('PASS: four synthetic business rows; Demo has no private records')
