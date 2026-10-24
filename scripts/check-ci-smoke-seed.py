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
