// Current React callsite discovery + reviewed evidence, never an API parity test.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import ts from 'typescript';
assert.ok(process.argv.length===2||process.argv.length===3&&process.argv[2]==='--write','Use only --write or no arguments');
const root=new URL('../',import.meta.url),read=p=>{assert.ok(!p.startsWith('/')&&!p.split('/').includes('..'));return fs.readFileSync(new URL(p,root),'utf8');};
const review=JSON.parse(read('docs/dev/API_COVERAGE_REVIEW.json'));
for(const [file,sha] of Object.entries(review.sourceHashes))assert.equal(createHash('sha256').update(read(file)).digest('hex'),sha,`Router/transport/source changed: ${file}; review API matrix then refresh its source hash`);
const known=new Map(review.families.map(f=>[f.id,f]));assert.equal(known.size,review.families.length);
function reference(r){assert.ok(!r.file.startsWith('/')&&!r.file.split('/').includes('..'));const body=read(r.file);if(r.symbol)assert.ok(body.includes('func '+r.symbol+'('),`Missing Go test reference: ${r.file} ${r.symbol}`);if(r.required)assert.ok(read('.github/workflows/mariadb-staging-ci.yml').includes(r.symbol),`SQL evidence is not an explicit required CI target: ${r.symbol}`);}
for(const f of review.families){assert.ok(f.gap&&f.level&&f.implementation);assert.ok(['partial','unported','dynamic'].includes(f.implementation));for(const r of f.evidence)reference(r);}
const files=[];function walk(dir){for(const e of fs.readdirSync(new URL(dir+'/',root),{withFileTypes:true}).sort((a,b)=>a.name<b.name?-1:1)){const p=dir+'/'+e.name;if(e.isDirectory())walk(p);else if(/\.tsx?$/.test(p))files.push(p);}}walk('frontend');walk('web');
const sites=[],bindings=[],forwarders=[];
function attrs(node){return new Map(node.attributes.properties.filter(ts.isJsxAttribute).map(a=>[a.name.getText(),a.initializer]));}
function value(node,source){if(!node)return '';if(ts.isJsxExpression(node))return node.expression?.getText(source)??'';return node.getText(source);}
function literals(node){const out=[];function visit(n){if(ts.isStringLiteral(n)||ts.isNoSubstitutionTemplateLiteral(n)||n.kind===ts.SyntaxKind.TemplateHead||n.kind===ts.SyntaxKind.TemplateMiddle||n.kind===ts.SyntaxKind.TemplateTail)out.push(n.text);ts.forEachChild(n,visit);}visit(node);return out;}
function family(url,hints){const ids=new Set();for(const p of hints){let id;
 if(p==='/auth'||p==='/logout'||p==='/api/session')id='session';
 else if(p.startsWith('/api/crm/applications')||p.startsWith('/api/applications'))id='applications';
 else if(p.startsWith('/api/crm'))id='crm';
 else if(p.startsWith('/api/companies/recalculate'))id='catalog-operations';
 else if(p.startsWith('/api/companies')||p.startsWith('/api/agencies'))id='catalog';
 else if(p.startsWith('/api/vacancies'))id=url.includes('cover-letter')?'cover-letter':url.includes('relevance')?'relevance':'vacancies';
 else if(p.startsWith('/api/'))id=p.slice(5).split(/[/?]/)[0];
 if(id){assert.ok(known.has(id),`New unreviewed API family ${id}`);ids.add(id);}}
 return ids.size?[...ids].sort():['dynamic'];}
function methodOf(name,args,source){if(name==='useApi'||name==='load')return ['GET','GET'];if(name==='mutate'){const n=args[1];return n&&ts.isStringLiteral(n)?[n.text,n.getText(source)]:['DYNAMIC',n?.getText(source)??'<missing>'];}const options=args[1];if(!options)return ['GET','GET'];if(!ts.isObjectLiteralExpression(options))return ['DYNAMIC',options.getText(source)];const prop=options.properties.find(p=>(ts.isPropertyAssignment(p)||ts.isShorthandPropertyAssignment(p))&&p.name.getText(source)==='method');if(prop){const n=ts.isPropertyAssignment(prop)?prop.initializer:prop.name;return ts.isStringLiteral(n)?[n.text,n.getText(source)]:['DYNAMIC',n.getText(source)];}return options.properties.some(ts.isSpreadAssignment)?['DYNAMIC',options.getText(source)]:['GET','GET'];}
function record(file,node,source,call,urlNode,method,methodExpression,target=sites){const url=value(urlNode,source),hints=literals(urlNode).filter(p=>p.startsWith('/api/')||p==='/auth'||p==='/logout');const line=source.getLineAndCharacterOfPosition(node.getStart(source)).line+1;const id=createHash('sha256').update(file+'\0'+call+'\0'+url+'\0'+methodExpression).digest('hex').slice(0,16);target.push({id,file,line,call,method,methodExpression,urlExpression:url,literalFragments:[...new Set(hints)],families:family(url,hints),dynamicURL:!ts.isStringLiteral(urlNode)&&!ts.isNoSubstitutionTemplateLiteral(urlNode)});}
for(const file of files){const source=ts.createSourceFile(file,read(file),ts.ScriptTarget.Latest,true,file.endsWith('.tsx')?ts.ScriptKind.TSX:ts.ScriptKind.TS);assert.equal(source.parseDiagnostics.length,0,`Cannot parse ${file}`);const imports=new Map();for(const n of source.statements)if(ts.isImportDeclaration(n)&&ts.isStringLiteral(n.moduleSpecifier)&&n.moduleSpecifier.text.endsWith('api/client')){assert.ok(!n.importClause?.name&&n.importClause?.namedBindings&&ts.isNamedImports(n.importClause.namedBindings),`Review client namespace/default transport import in ${file}`);for(const e of n.importClause.namedBindings.elements){const name=e.propertyName?.text??e.name.text;assert.ok(review.clientExports.includes(name),`Unreviewed client import ${name}`);imports.set(e.name.text,name);}}
 // The central module is represented by the explicit reviewed transport list,
 // not counted again for every forwarded url/key. New wrappers require review.
 if(file==='frontend/api/client.ts'){const exported=[];for(const n of source.statements)if(n.modifiers?.some(m=>m.kind===ts.SyntaxKind.ExportKeyword)){if(ts.isFunctionDeclaration(n)||ts.isClassDeclaration(n))exported.push(n.name.text);else if(ts.isVariableStatement(n))exported.push(...n.declarationList.declarations.map(d=>d.name.getText(source)));}assert.deepEqual(exported.slice().sort(),review.clientExports.slice().sort(),'Review changed client exports');continue;}
 function visit(n){
  if(ts.isCallExpression(n)&&n.arguments[0]){const expression=n.expression.getText(source),call=expression==='fetch'?'fetch':imports.get(expression);if(['request','mutate','load','useApi','fetch'].includes(call)){const [method,expr]=methodOf(call,n.arguments,source);record(file,n,source,call,n.arguments[0],method,expr);}}
  if(ts.isJsxOpeningElement(n)||ts.isJsxSelfClosingElement(n)){const a=attrs(n),tag=n.tagName.getText(source);if(tag==='form'&&a.get('action')){const method=(a.get('method')?.text??'get').toUpperCase();record(file,n,source,'form',a.get('action'),method,method);}
   if(tag==='a'&&a.get('href')&&literals(a.get('href')).some(p=>p==='/logout'||p.startsWith('/api/')))record(file,n,source,'link',a.get('href'),'GET','GET');
   // url props close the known download/editor forwarding gap. A binding is
   // shown separately; it is not guessed to be an independent HTTP request.
   if(a.get('url')&&literals(a.get('url')).some(p=>p.startsWith('/api/')))record(file,n,source,'prop:'+tag,a.get('url'),'BOUND','component transport',bindings);
  }
  if(ts.isCallExpression(n)&&ts.isIdentifier(n.expression)&&n.expression.text==='download'&&n.arguments[0])record(file,n,source,'download argument',n.arguments[0],'BOUND','download forwarding',forwarders);
  ts.forEachChild(n,visit);
 }visit(source);
}
for(const list of [sites,bindings,forwarders]){const seen=new Map();for(const s of list){const occurrence=(seen.get(s.id)??0)+1;seen.set(s.id,occurrence);if(occurrence>1)s.id+='-'+occurrence;}}
const snapshot={scope:'Current frontend/web direct transport sites and reviewed JSX/download bindings; no runtime/golden coverage inferred',typescript:ts.version,files:files.length,transportModule:'frontend/api/client.ts',sites,bindings,forwarders};
const row=(label,n)=>`| ${label} | ${n} |`;
const md=['# Покрытие API текущего React-клиента','',
'GO-314 — воспроизводимый аудит вызовов, не приёмка полного GO-002. Источники: frontend/web AST, Go router, явно перечисленные test references. Проверяется `node scripts/check-api-coverage.mjs`; после осмысленного review обновление — `--write`. Генератор не выполняет HTTP, SQL или providers.','',
'| Инвентарь | Количество |','|---|---|',row('TS/TSX файлов',files.length),row('Прямых transport/form/link callsites',sites.length),row('JSX url bindings',bindings.length),row('Download forwarding sites',forwarders.length),row('Callsites с динамическим URL',sites.filter(s=>s.dynamicURL).length),row('Callsites с динамическим методом',sites.filter(s=>s.method==='DYNAMIC').length),'',
'Число callsites не равно числу endpoints или завершённых задач. Nullable useApi может не отправлять запрос; conditional URL/method сохраняются как выражения без выдуманного декартова произведения. Binding и forwarding не считаются дополнительным подтверждённым запросом. Полные выражения и места находятся в [API_CALLSITES.json](API_CALLSITES.json).','',
'| Семейство | Go HTTP | Подтверждённый уровень | Открытая часть | Ссылки |','|---|---|---|---|---|',
...review.families.map(f=>`| ${f.id} | ${f.implementation} | ${f.level} | ${f.gap} | ${f.evidence.map(r=>`[${r.symbol??r.file}](../../${r.file})`).join(', ')} |`),'',
'Go HTTP partial означает только явно реализованный subset при переданных зависимостях. Unit-тест, наличие handler или pure source validator не подменяют actual SQL/HTTP parity. Для unported семейств Go router оставляет authenticated fail-closed ответ; методы/ошибки полной исходной реализации ещё не перенесены.','',
'Все references проверяются на существование, SQL parents — также на явное включение в required workflow. Это подтверждает связь с проверкой, не доказывает покрытие каждого URL/метода: строка gap сохраняется даже при зелёном CI.','',
'Границы discovery: named imports request/mutate/load/useApi (включая import aliases), direct fetch, form/link и JSX url/download forwarding. Внешние SDK, произвольные новые wrapper/alias/data-flow и серверные automation/MCP endpoints не выводятся автоматически. Namespace/default imports клиента отвергаются до review. Хеши центрального transport, Go router и legacy route source закреплены в API_COVERAGE_REVIEW.json: изменение требует проверки matrix и осмысленного обновления хеша, а не только --write.','',
'Очередь critical работ и внешние блокировки — [PLANS.md](../../PLANS.md), результаты PR/CI — [tasks/STACK.md](../../tasks/STACK.md), исходный полный server inventory — [LEGACY_API_BASELINE.md](LEGACY_API_BASELINE.md). Полный source HTTP golden, catalog/vacancy filters/null/flags, остальные negative rights и pipeline/import остаются открытыми; статический аудит не закрывает их.',''].join('\n');
for(const [file,content] of [['docs/dev/API_CALLSITES.json',JSON.stringify(snapshot,null,2)+'\n'],['docs/dev/API_COVERAGE.md',md]]){if(process.argv.includes('--write'))fs.writeFileSync(new URL(file,root),content);else assert.equal(read(file),content,`API inventory/review drift: ${file}; review then regenerate with --write`);}
console.log(`PASS: ${sites.length} frontend transport sites,${bindings.length} URL bindings,${forwarders.length} forwarding sites; ${review.families.length} reviewed evidence/gap families; no runtime parity claim`);
