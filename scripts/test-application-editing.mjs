import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.localStorage={getItem:()=>null,setItem(){}};
const oldFetch=fetch;
let root,sent,saved=0,allowSuccess=false,detailStatus=200,removed=false,puts=0,listStatus=200;
const initial={id:17,revision:'a'.repeat(64),companyName:'Synthetic',vacancyId:null,vacancyTitle:'',status:'Откликнулся',comment:'Initial comment',recruiterName:'Initial contact',recruiterPhone:'',recruiterEmail:'',recruiterContactType:'',applicationSources:['hcm'],history:[]};
let current={...initial};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const mount=async(component,props)=>{await act(async()=>{root=create(h(component,props));});await flush();};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null;ui.clearCache();};
const submit=()=>act(async()=>root.root.findByType('form').props.onSubmit({preventDefault(){}}));
const change=(node,value)=>act(async()=>node.props.onChange({target:{value}}));
const text=node=>typeof node==='string'?node:Array.isArray(node)?node.map(text).join(''):node?.children?text(node.children):'';
const button=label=>root.root.findAllByType('button').find(node=>text(node)===label);
const click=node=>act(async()=>node.props.onClick());
const reload=async()=>{await click(button('Загрузить актуальный отклик'));await flush();};
const apply=async()=>{assert.equal(button('Применить к форме').props.disabled,false);await click(button('Применить к форме'));};
try {
 globalThis.fetch=async(url,options={})=>{
  if(!options.method){
   if(url==='/api/applications/17')return Response.json(detailStatus===200?{record:current}:{error:detailStatus===404?'Отклик удалён':'Ошибка загрузки'},{status:detailStatus});
   if(listStatus!==200)return Response.json({error:'Не удалось обновить список'},{status:listStatus});
   return Response.json({records:removed?[]:[current],entities:[{name:'Synthetic',entityType:'company'}],vacancies:[]});
  }
  sent={method:options.method,body:JSON.parse(options.body)};puts++;
  if(allowSuccess&&sent.body.revision===current.revision){current={...current,...sent.body,revision:'d'.repeat(64)};return Response.json({records:[current]});}
  return Response.json({error:'Отклик изменён в другой вкладке'},{status:409});
 };
 // A parent refresh must not attach a new token to an existing full-form draft.
 const props={companyName:initial.companyName,record:initial,onSaved:()=>saved++};
 await mount(ui.ApplicationForm,props);await change(root.root.findByType('textarea'),'Full form draft');
 current={...initial,revision:'b'.repeat(64),status:'Офер'};
 await act(async()=>root.update(h(ui.ApplicationForm,{...props,record:current})));
 await submit();assert.equal(sent.body.revision,initial.revision);assert.equal(sent.body.status,initial.status);assert.equal(root.root.findByType('textarea').props.value,'Full form draft');assert.equal(saved,0);assert.match(JSON.stringify(root.toJSON()),/изменён в другой вкладке/);
 const beforeResolve=puts;await reload();await apply();assert.equal(puts,beforeResolve,'Resolve must not automatically save');
 assert.equal(root.root.findByType('textarea').props.value,'Full form draft');assert.equal(root.root.findByProps({'aria-label':'Статус отклика'}).props.value,'Офер');
 allowSuccess=true;await submit();assert.equal(sent.body.revision,'b'.repeat(64));assert.equal(sent.body.status,'Офер');assert.equal(saved,1);await stop();allowSuccess=false;
 // A list refresh while editing preserves both comment and its original snapshot.
 current={...initial};await mount(ui.Applications,{nav});
 await act(async()=>root.root.findByProps({className:'application-comment rendered-md'}).props.onClick());
 await change(root.root.findByType('textarea'),'Inline draft');current={...current,revision:'b'.repeat(64),status:'Офер'};
 await act(async()=>ui.invalidate(key=>key==='/api/crm/applications'));await flush();
 await submit();assert.equal(sent.body.revision,initial.revision);assert.equal(sent.body.status,initial.status);assert.equal(root.root.findByType('textarea').props.value,'Inline draft');
 await submit();assert.equal(sent.body.revision,initial.revision,'Retry must still use original snapshot');await stop();
 current={...initial};await mount(ui.Applications,{nav});
 await act(async()=>root.root.findByProps({'aria-label':'Редактировать контакты рекрутера'}).props.onClick());
 await change(root.root.findAllByType('input').find(node=>node.props.maxLength===160),'Contact draft');
 current={...current,revision:'c'.repeat(64),comment:'Fresh comment'};
 await act(async()=>ui.invalidate(key=>key==='/api/crm/applications'));await flush();await submit();
 assert.equal(sent.body.revision,initial.revision);assert.equal(sent.body.comment,initial.comment);assert.equal(sent.body.recruiterName,'Contact draft');assert.equal(root.root.findAllByType('input').find(node=>node.props.maxLength===160).props.value,'Contact draft');await stop();
 // Explicit status changes use the current displayed row snapshot.
 await mount(ui.Applications,{nav});await change(root.root.findByProps({className:'application-status'}),'Офер');assert.equal(sent.body.revision,current.revision);await stop();
 // A rejected/removed row must not unmount an active inline draft on refresh.
 current={...initial};await mount(ui.Applications,{nav});
 await act(async()=>root.root.findByProps({className:'application-comment rendered-md'}).props.onClick());await change(root.root.findByType('textarea'),'Pinned draft');
 listStatus=503;await act(async()=>ui.invalidate(key=>key==='/api/crm/applications'));await flush();assert.equal(root.root.findByType('textarea').props.value,'Pinned draft');listStatus=200;
 current={...current,status:'Отказ',revision:'b'.repeat(64)};await act(async()=>ui.invalidate(key=>key==='/api/crm/applications'));await flush();
 assert.equal(root.root.findByType('textarea').props.value,'Pinned draft');await submit();await reload();await apply();
 allowSuccess=true;await submit();await flush();assert.equal(sent.body.comment,'Pinned draft');assert.equal(sent.body.status,'Отказ');assert.equal(root.root.findAllByType('textarea').length,0);await stop();allowSuccess=false;
 // Same-field conflicts require an explicit choice; changing a draft clears that choice.
 current={...initial};await mount(ui.ApplicationForm,{companyName:'Synthetic',record:initial});await change(root.root.findByType('textarea'),'My comment');
 current={...current,comment:'Other comment',recruiterPhone:'Fresh phone',revision:'b'.repeat(64)};await submit();await reload();
 assert.equal(button('Применить к форме').props.disabled,true);
 await change(root.root.findByProps({'aria-label':'Согласовать: Комментарий'}),'draft');assert.equal(button('Применить к форме').props.disabled,false);
 await change(root.root.findByType('textarea'),'Revised draft');await flush();assert.equal(button('Применить к форме').props.disabled,true);
 await change(root.root.findByProps({'aria-label':'Согласовать: Комментарий'}),'draft');await apply();
 assert.equal(root.root.findByType('textarea').props.value,'Revised draft');assert.equal(root.root.findAllByType('input').find(node=>node.props.maxLength===64).props.value,'Fresh phone');
 // A third writer after reconciliation still causes a new conflict and keeps the draft.
 current={...current,status:'Скрининг',revision:'c'.repeat(64)};allowSuccess=true;await submit();assert.equal(root.root.findByType('textarea').props.value,'Revised draft');await reload();await apply();await submit();
 assert.equal(sent.body.revision,'c'.repeat(64));assert.equal(sent.body.status,'Скрининг');assert.equal(sent.body.comment,'Revised draft');await stop();allowSuccess=false;
 // Choosing the current field is an explicit discard of just that conflicting field.
 current={...initial};await mount(ui.ApplicationForm,{companyName:'Synthetic',record:initial});await change(root.root.findByType('textarea'),'My discarded draft');
 current={...current,comment:'Current accepted comment',revision:'b'.repeat(64)};await submit();await reload();await change(root.root.findByProps({'aria-label':'Согласовать: Комментарий'}),'current');await apply();
 assert.equal(root.root.findByType('textarea').props.value,'Current accepted comment');await stop();
 // Contact reconciliation preserves fresh phone/comment while retaining the edited name.
 current={...initial};await mount(ui.Applications,{nav});await click(root.root.findByProps({'aria-label':'Редактировать контакты рекрутера'}));
 await change(root.root.findAllByType('input').find(node=>node.props.maxLength===160),'My name');
 current={...current,recruiterPhone:'Current phone',comment:'Current note',revision:'b'.repeat(64)};await submit();await reload();await apply();allowSuccess=true;await submit();
 assert.equal(sent.body.recruiterName,'My name');assert.equal(sent.body.recruiterPhone,'Current phone');assert.equal(sent.body.comment,'Current note');await stop();allowSuccess=false;
 // The immediate status action retains its intended selection after a 409.
 current={...initial};await mount(ui.Applications,{nav});current={...current,status:'Скрининг',revision:'b'.repeat(64)};
 await change(root.root.findByProps({className:'application-status'}),'Офер');assert.equal(root.root.findByProps({className:'application-status'}).props.value,'Офер');
 await reload();assert.equal(button('Применить к форме').props.disabled,true);await change(root.root.findByProps({'aria-label':'Согласовать: Статус отклика'}),'draft');await apply();allowSuccess=true;await submit();assert.equal(sent.body.status,'Офер');assert.equal(sent.body.revision,'b'.repeat(64));await stop();allowSuccess=false;
 // Failed or deleted detail reloads cannot discard the draft or enable a save.
 current={...initial};await mount(ui.ApplicationForm,{companyName:'Synthetic',record:initial});await change(root.root.findByType('textarea'),'Keep on failure');await submit();
 for(const status of [503,404]){detailStatus=status;await reload();assert.equal(root.root.findByType('textarea').props.value,'Keep on failure');assert.equal(button('Применить к форме'),undefined);assert.equal(root.root.findByProps({className:'crm-save'}).props.disabled,true);}await stop();detailStatus=200;
 // A selected company form remains mounted even if the server deletes its row.
 current={...initial};await mount(ui.CompanyApplications,{companyName:'Synthetic'});await click(root.root.findAllByType('button').find(node=>String(node.props.className).includes('company-application-card')));await change(root.root.findByType('textarea'),'Deleted record draft');
 listStatus=503;await act(async()=>ui.invalidate(key=>key.startsWith('/api/applications?')));await flush();assert.equal(root.root.findByType('textarea').props.value,'Deleted record draft');listStatus=200;
 removed=true;await act(async()=>ui.invalidate(key=>key.startsWith('/api/applications?')));await flush();assert.equal(root.root.findByType('textarea').props.value,'Deleted record draft');await stop();removed=false;
 // All editable fields use the same three-way merge, including set-like sources.
 const latest={...initial,revision:'b'.repeat(64),status:'Офер',recruiterPhone:'Latest',applicationSources:['hh','hcm']};
 const merged=ui.reconcileApplication(initial,{...initial,comment:'Mine'},latest);assert.equal(merged.comment,'Mine');assert.equal(merged.status,'Офер');assert.equal(merged.recruiterPhone,'Latest');assert.deepEqual(merged.applicationSources,['hh','hcm']);
 assert.deepEqual(ui.applicationConflicts({...initial,applicationSources:['hh','hcm']},{...initial,applicationSources:['hcm','hh']},latest),[]);
 assert.throws(()=>ui.reconcileApplication(initial,{...initial,comment:'Mine'},{...latest,comment:'Other'}),/Выберите значение/);
 console.log('Actual React: frozen drafts, explicit three-way reconciliation, same-field choices, repeat conflict, pinned rejected/deleted rows, contact/status recovery and failed reload retention passed.');
} finally {await stop();globalThis.fetch=oldFetch;}
