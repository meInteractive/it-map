#!/usr/bin/env python3
"""Verify schema-only CI fixture against the non-private Go schema baseline.

This reads only the schema fixture and baseline; it never opens a dump, connects
 to a database, or evaluates application data.
"""
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[1]
sql = (root / 'db/testdata/empty-schema.sql').read_text(encoding='utf-8')
baseline = json.loads((root / 'internal/storage/baseline.json').read_text(encoding='utf-8'))['tables']

if re.search(r'^\s*(?:INSERT|REPLACE|UPDATE|DELETE|DROP|CREATE USER|GRANT)\b', sql, re.I | re.M):
    raise SystemExit('FAIL: non-schema SQL statement present in CI fixture')
if 'CONVERT(X\'' in sql or re.search(r'https?://|@gmail\.com', sql, re.I):
    raise SystemExit('FAIL: possible row data in CI fixture')

actual = {}
table = None
for line in sql.splitlines():
    match = re.match(r'^CREATE TABLE `([^`]+)` \(', line)
    if match:
        if table is not None:
            raise SystemExit('FAIL: nested CREATE TABLE')
        table = match.group(1)
        actual[table] = {}
        continue
    if table is not None:
        col = re.match(r'^  `([^`]+)` ([A-Za-z]+)', line)
        if col:
            actual[table][col.group(1)] = col.group(2).lower()
        if line.startswith(') ENGINE='):
            table = None

if table is not None:
    raise SystemExit('FAIL: unclosed CREATE TABLE')
if actual.keys() != baseline.keys() or any(
    actual[t].keys() != baseline[t].keys() or any(
        expected != '*' and actual[t][name] != expected
        for name, expected in columns.items()
    )
    for t, columns in baseline.items()
):
    raise SystemExit('FAIL: CI fixture differs from embedded Go MariaDB baseline')
if len(actual) != 72 or sum(map(len, actual.values())) != 606:
    raise SystemExit('FAIL: unexpected schema size')
triggers = len(re.findall(r'^CREATE TRIGGER ', sql, re.M))
foreign_keys = len(re.findall(r'^ALTER TABLE .* ADD CONSTRAINT .* FOREIGN KEY ', sql, re.M))
if triggers != 22 or foreign_keys != 50:
    raise SystemExit(f'FAIL: expected 22 triggers and 50 foreign keys, got {triggers} and {foreign_keys}')
# Keep the embedded Go object manifest synchronized with the fixture. This
# catches drift before any CI runner attempts an SQL import or connects to DB.
object_manifest = json.loads((root / 'internal/storage/schema-objects.json').read_text(encoding='utf-8'))
source_foreign_keys = sorted(
    ({'table': table, 'name': name} for table, name in
     re.findall(r'^ALTER TABLE `([^`]+)` ADD CONSTRAINT `([^`]+)` FOREIGN KEY ', sql, re.M)),
    key=lambda obj: (obj['table'], obj['name']))
source_triggers = sorted(
    ({'table': table, 'name': name, 'timing': timing, 'event': event}
     for name, timing, event, table in re.findall(
         r'^CREATE TRIGGER `([^`]+)` (BEFORE|AFTER) (INSERT|UPDATE|DELETE) ON `([^`]+)` FOR EACH ROW', sql, re.M)),
    key=lambda obj: (obj['table'], obj['name']))
if object_manifest != {'foreign_keys': source_foreign_keys, 'triggers': source_triggers}:
    raise SystemExit('FAIL: embedded schema-object manifest differs from the CI SQL fixture')
print('PASS: empty MariaDB CI schema matches 72 tables/606 columns; 22 triggers, 50 FKs; no row SQL')
