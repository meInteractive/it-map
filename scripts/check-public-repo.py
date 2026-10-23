#!/usr/bin/env python3
"""Fail-closed basic public repository scan. Does not replace secret audit."""
from pathlib import Path
import re,sys
root=Path(__file__).resolve().parents[1]
banned_paths=['dist/','.openai/','.vinext/','worker/','drizzle/']
token_rules={
  'private-key':r'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----',
  'github-token':r'\b(?:gh[pousr]_[a-zA-Z0-9_]{20,}|github_pat_[a-zA-Z0-9_]{20,})',
  'openai-token':r'\bsk-(?:proj-)?[A-Za-z0-9_-]{24,}',
  'apify-token':r'\bapify_api_[A-Za-z0-9]{20,}',
  'google-api-key':r'\bAIza[0-9A-Za-z_-]{30,}',
  'aws-key-id':r'\b(?:AKIA|ASIA)[A-Z0-9]{16}',
  'db-credentials-in-url':r'(?i)\b(?:mysql|mariadb|postgres|mongodb)(?:\+srv)?://[^\s]+:[^\s]+@',
  'sites-project-identity':r'\bappgprj_[0-9a-f]{20,}',
}
issues=[]
for f in root.rglob('*'):
  if not f.is_file(): continue
  name=f.relative_to(root).as_posix()
  if name.startswith(('.git/','node_modules/')):continue
  if any(name.startswith(prefix) for prefix in banned_paths):issues.append((name,'banned directory'))
  if f.name.endswith(('.sqlite','.sqlite3','.db','.dump','.bak','.pem','.key','.p12','.pfx')):
    issues.append((name,'banned extension'))
  if name != 'server/generated/backup-schema.json' and re.search(r'(?i)(backup|snapshot|d1-original|mariadb).*(\.json|\.sql|\.zip|\.gz)$',name):
    issues.append((name,'data archive or snapshot'))
  if f.suffix.lower() in ('.png','.jpg','.jpeg','.ico','.webp'):continue
  try: data=f.read_text(encoding='utf8')
  except UnicodeError: continue
  for rule,expr in token_rules.items():
    if re.search(expr,data):issues.append((name,rule))
for path,reason in issues: print(f'BLOCKED: {path} ({reason})')
if issues:sys.exit(1)
print('PASS: static source gate (not a guarantee of zero secrets or PII)')
