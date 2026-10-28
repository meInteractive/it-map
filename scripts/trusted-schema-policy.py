#!/usr/bin/env python3
"""Build schema-only trusted DDL actions; never opens a dump or a database."""
import argparse,hashlib,json,re
from pathlib import Path
root=Path(__file__).resolve().parents[1]
source=(root/'db/testdata/empty-schema.sql').read_text()
actions=[]
for sql in re.findall(r'^CREATE TABLE `[^`]+` \(\n.*?^\) ENGINE=[^\n]+;',source,re.M|re.S):
 name=re.match(r'CREATE TABLE `([^`]+)`',sql)[1]
 actions.append(dict(kind='table',table=name,name=name,sql=sql))
for line in source.splitlines():
 m=re.fullmatch(r'CREATE (?:UNIQUE )?INDEX `([^`]+)` ON `([^`]+)`.*;',line)
 if m:actions.append(dict(kind='index',table=m[2],name=m[1],sql=line));continue
 m=re.fullmatch(r'ALTER TABLE `([^`]+)` ADD CONSTRAINT `([^`]+)` FOREIGN KEY .*;',line)
 if m:actions.append(dict(kind='foreign_key',table=m[1],name=m[2],sql=line));continue
 m=re.fullmatch(r'CREATE TRIGGER `([^`]+)` (?:BEFORE|AFTER) (?:INSERT|UPDATE|DELETE) ON `([^`]+)` .*\$\$',line)
 if m:actions.append(dict(kind='trigger',table=m[2],name=m[1],sql=line[:-2]))
assert len(actions)==217
assert {k:sum(a['kind']==k for a in actions) for k in ['table','index','foreign_key','trigger']}=={'table':72,'index':73,'foreign_key':50,'trigger':22}
assert len({(a['kind'],a['table'],a['name']) for a in actions})==217
# Metadata is captured from the same reviewed fixture in guarded read-only CI;
# never manufacture server-normalized defaults/expressions from a SQL regex.
path=root/'internal/dbschema/baseline_v1.json'
metadata=json.loads(path.read_text()).get('metadata') if path.exists() else None
policy=dict(version=1,sourceSHA256=hashlib.sha256(source.encode()).hexdigest(),actions=actions,metadata=metadata)
text=json.dumps(policy,ensure_ascii=False,separators=(',',':'))+'\n'
if '--write' in __import__('sys').argv:path.write_text(text)
else:assert path.read_text()==text,'Trusted DDL source drift'
print('PASS: trusted217 DDL actions match reviewed72-table schema-only source')
