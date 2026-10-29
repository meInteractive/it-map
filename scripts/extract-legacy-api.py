#!/usr/bin/env python3
"""Static legacy endpoint index: discovery only, not a contract verifier."""
from pathlib import Path
import re,json
root=Path(__file__).resolve().parents[1]
files=[root/'server/routes/index.js',*sorted((root/'server/services').glob('*route*.js')),*sorted((root/'server/integrations').glob('*.js'))]
string_path=re.compile(r'''["'`](/(?:api|auth|logout|mcp)(?:/[A-Za-z0-9_:{}.$()+?/-]*)?)''')
rows={}
for f in files:
 if not f.exists():continue
 data=f.read_text('utf-8')
 for match in string_path.finditer(data):
  path=match.group(1).rstrip('/\'"`')
  if not path or path.startswith('//'):continue
  relative=f.relative_to(root).as_posix()
  idx=data.count('\n',0,match.start())+1
  rows.setdefault(path,set()).add((relative,idx))
output={'description':'Static paths found in legacy code; dynamic regex routes and methods require manual review.',
        'paths':[{'path':p,'locations':[{'file':f,'line':line} for f,line in sorted(locs)]} for p,locs in sorted(rows.items())]}
(root/'docs/dev/LEGACY_API_PATHS.json').write_text(json.dumps(output,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
print('Indexed',len(rows),'candidate path literals; regex/dynamic routes may be omitted')
