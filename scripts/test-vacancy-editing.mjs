import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act, create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';
const ui = await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = {addEventListener(){}, removeEventListener(){}};
globalThis.document = {activeElement:null};
const oldFetch = fetch, oldConfirm = globalThis.confirm;
const base = {id:71, revision:'a'.repeat(64), companyName:'Original owner', entityType:'company', title:'Original title', url:'https://synthetic.test/job', role:'Менеджер проектов', level:'Senior', salaryFrom:200000, salaryTo:300000, relevance:50, descriptionMd:'## Original full text', coverLetterSubject:'Private subject', coverLetterMd:'Private original', coverLetterScope:'personal', publishedAt:'2026-10-01', loadedAt:'2026-10-02', telegramSources:[{channelId:7, channelName:'Original channel', messageUrl:'https://t.me/original/1', telegramContact:'@original'}]};
let root, current = {...base, revision:'b'.repeat(64), companyName:'New owner', title:'Fresh title', descriptionMd:'## Fresh description', coverLetterMd:'Fresh shared', coverLetterScope:'shared', relevance:0}, status = 409, readStatus = 200, sent = [], reads = 0, saved = 0, closed = 0, pendingPut, pendingRead;
const props = record => ({owner:{name:record.companyName, entityType:record.entityType}, record, onSaved(){saved++;}, onClose(){closed++;}});
const text = n => typeof n === 'string' ? n : Array.isArray(n) ? n.map(text).join('') : n?.children ? text(n.children) : '';
const button = label => root.root.findAllByType('button').find(n => text(n) === label);
const flush = () => act(async()=>{await new Promise(r=>setTimeout(r,0));});
const mount = async record => {await act(async()=>{root=create(h(ui.VacancyEditor, props(record)));});await flush();};
const stop = async()=>{if(root)await act(async()=>root.unmount());root=null;ui.clearCache();};
const change = (node, value) => act(async()=>node.props.onChange({target:{value}}));
const submit = () => act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
const click = node => act(async()=>node.props.onClick());
const draft = () => root.root.findAll(n => ['input','select','textarea'].includes(n.type) && !n.props.readOnly).map(n => [n.type,n.props.value]);
const markdown = label => root.root.findByProps({'aria-label':label});
try {
 globalThis.confirm=()=>false;
 globalThis.fetch=async(url, options={})=>{
  if(url==='/api/channels')return Response.json({channels:[{id:7,name:'Original channel'}]});
  assert.equal(url,'/api/vacancies/71');
  if(options.method==='PUT'){
   sent.push(JSON.parse(options.body));
   if(pendingPut)return new Promise(resolve=>{pendingPut=resolve;});
   return Response.json(status===200?{vacancies:[current]}:{error:'Synthetic conflict or error'},{status});
  }
  assert.ok(!options.method || options.method==='GET'); reads++;
  if(pendingRead)return new Promise(resolve=>{pendingRead=resolve;});
  return Response.json(readStatus===200?{vacancy:current}:{error:readStatus===404?'Вакансия удалена':'Не удалось загрузить актуальную версию'},{status:readStatus});
 };
 // Props refresh cannot replace original owner/revision or promote edited private text.
 await mount(base);assert.equal(reads,0);
 await change(root.root.findAllByType('input').find(n=>n.props.value===base.title),'Draft title');
 await change(markdown('Полный текст вакансии'),'## Long draft\n'+'x'.repeat(9000));
 await change(markdown('Текст письма'),'Private draft');
 const before=draft();await act(async()=>root.update(h(ui.VacancyEditor,props(current))));
 assert.deepEqual(draft(),before);assert.match(text(root.root),/Личное письмо/);
 await submit();assert.equal(sent.at(-1).revision,base.revision);assert.equal(sent.at(-1).companyName,base.companyName);assert.equal(sent.at(-1).coverLetterScope,'personal');assert.equal(sent.at(-1).coverLetterMd,'Private draft');assert.deepEqual(draft(),before);assert.equal(saved,0);assert.equal(reads,0,'409 must not auto-fetch or auto-save');
 await click(button('Показать актуальную версию'));assert.equal(reads,1);assert.deepEqual(draft(),before);assert.equal(sent.length,1);
 const region=root.root.findByProps({'aria-label':'Актуальная версия вакансии'});assert.match(text(region),/New owner/);assert.match(text(region),/Fresh title/);assert.match(text(region),/Общее, видно всем/);assert.equal(region.findAllByType('textarea').length,2);assert.ok(region.findAllByType('textarea').every(n=>n.props.readOnly && !n.props.onChange));assert.equal(markdown('Сохранённое письмо').props.value,current.coverLetterMd);
 await submit();assert.equal(sent.length,2);assert.equal(sent.at(-1).revision,base.revision,'Read-only latest must not lend a fresh token to stale draft');assert.equal(sent.at(-1).coverLetterScope,'personal');assert.deepEqual(draft(),before);
 for(readStatus of [503,404]){await click(button('Показать актуальную версию'));assert.deepEqual(draft(),before);assert.equal(root.root.findAllByProps({'aria-label':'Актуальная версия вакансии'}).length,0);assert.equal(saved,0);}
 readStatus=200;status=428;await submit();assert.deepEqual(draft(),before);assert.ok(button('Показать актуальную версию'));
 // Delayed duplicate reads coalesce, and a draft typed during GET is retained.
 pendingRead=true;let firstRead, secondRead;const readsBefore=reads;
 await act(()=>{const handler=button('Показать актуальную версию').props.onClick;firstRead=handler();secondRead=handler();});
 assert.equal(reads,readsBefore+1);assert.equal(button('Загрузка актуальной версии…').props.disabled,true);
 await change(markdown('Полный текст вакансии'),'## Draft typed during GET');
 await act(async()=>{pendingRead(Response.json({vacancy:current}));pendingRead=undefined;await Promise.all([firstRead,secondRead]);});
 assert.equal(markdown('Полный текст вакансии').props.value,'## Draft typed during GET');assert.equal(sent.at(-1).revision,base.revision);
 // Delayed PUT locks native fields/view/close and rejects duplicate submission.
 pendingPut=true;let firstPut, secondPut;const putsBefore=sent.length;
 await act(()=>{const handler=root.root.findByType('form').props.onSubmit;firstPut=handler({preventDefault(){}});secondPut=handler({preventDefault(){}});});
 assert.equal(sent.length,putsBefore+1);assert.equal(root.root.findByType('fieldset').props.disabled,true);assert.equal(button('Отмена').props.disabled,true);assert.equal(button('Показать актуальную версию').props.disabled,true);
 await click(root.root.findByProps({'aria-label':'Закрыть'}));assert.equal(closed,0);
 await act(async()=>{pendingPut(Response.json({error:'Conflict after delay'},{status:409}));pendingPut=undefined;await Promise.all([firstPut,secondPut]);});
 assert.equal(root.root.findByType('fieldset').props.disabled,false);assert.equal(markdown('Полный текст вакансии').props.value,'## Draft typed during GET');
 await click(button('Отмена'));assert.equal(closed,0,'Discard must be confirmed');globalThis.confirm=()=>true;await click(button('Отмена'));assert.equal(closed,1);await stop();
 // An unchanged personal letter is still omitted after shared props refresh.
 status=409;await mount(base);await change(markdown('Полный текст вакансии'),'Unrelated draft');await act(async()=>root.update(h(ui.VacancyEditor,props(current))));await submit();assert.equal(sent.at(-1).revision,base.revision);assert.ok(!Object.hasOwn(sent.at(-1),'coverLetterMd'));assert.ok(!Object.hasOwn(sent.at(-1),'coverLetterSubject'));assert.ok(!Object.hasOwn(sent.at(-1),'coverLetterScope'));await stop();
 // The reverse scope transition cannot turn an explicit shared edit into personal.
 const shared={...base,coverLetterScope:'shared'};await mount(shared);await change(markdown('Текст письма'),'Shared draft');await act(async()=>root.update(h(ui.VacancyEditor,props({...current,coverLetterScope:'personal'}))));await submit();assert.equal(sent.at(-1).coverLetterScope,'shared');assert.equal(sent.at(-1).revision,base.revision);await stop();
 // A legacy detail cannot bypass CAS or silently obtain a new token for its draft.
 const {revision,...legacy}=base;const puts=sent.length;await mount(legacy);await change(markdown('Полный текст вакансии'),'Legacy draft');await submit();assert.equal(sent.length,puts);await click(button('Показать актуальную версию'));await submit();assert.equal(sent.length,puts);assert.equal(markdown('Полный текст вакансии').props.value,'Legacy draft');await stop();
 // Other errors do not pretend to be concurrency conflicts; success still works.
 status=400;await mount(base);await submit();assert.equal(button('Показать актуальную версию'),undefined);assert.equal(markdown('Полный текст вакансии').props.value,base.descriptionMd);status=200;await submit();assert.equal(saved,1);await stop();
 console.log('Actual React VacancyEditor: frozen owner/revision/letter scope, unchanged letter omission, 409/428/legacy retained drafts, read-only uncached latest, read errors/deletion/delays, duplicate guards, saving lock and confirmed discard passed.');
} finally {await stop();globalThis.fetch=oldFetch;globalThis.confirm=oldConfirm;}
