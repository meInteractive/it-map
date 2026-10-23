import assert from 'node:assert/strict';
import {createElement as h,useState} from 'react';
import {create,act} from 'react-test-renderer';
import {moduleFor,nav} from './test-support.mjs';
const ui=await moduleFor('scripts/react-test-entry.tsx');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.localStorage={getItem:()=>null,setItem(){}};
const oldFetch=fetch;let root,release,pending,requests,sent,failed,busyEvents;
const record={id:99,revision:'a'.repeat(64),companyName:'Synthetic',vacancyId:null,vacancyTitle:'',status:'Откликнулся',comment:'Before',recruiterName:'',recruiterPhone:'',recruiterEmail:'',recruiterContactType:'',applicationSources:[],history:[]};
const flush=()=>act(async()=>{await new Promise(resolve=>setTimeout(resolve,0));});
const mount=async(component,props)=>{requests=0;sent=null;failed=false;busyEvents=[];await act(async()=>{root=create(h(component,props));});await flush();};
const stop=async()=>{if(root)await act(async()=>root.unmount());root=null;ui.clearCache();};
function disabled(node) {for(let n=node;n;n=n.parent)if(typeof n.type==='string'&&(n.props.disabled||n.props.inert))return true;return false;}
async function type(node,value) {assert.equal(disabled(node),false,'Simulated user cannot edit a disabled field');await act(async()=>node.props.onChange({target:{value}}));}
function assertLocked() {
 const form=root.root.findByType('form');
 const controls=[...form.findAllByType('input'),...form.findAllByType('textarea'),...form.findAllByType('select')].filter(n=>n.findAllByType('option').length===0||n.type==='select');
 assert.ok(controls.length);for(const node of controls)assert.ok(disabled(node),'Every application input must be natively locked while saving');
 assert.equal(root.root.findByType('form').props['aria-busy'],true);
 for(const legend of form.findAllByType('legend'))assert.equal(legend.findAll(n=>['input','select','textarea','button','a'].includes(n.type)).length,0,'No editable controls in a disabled-fieldset legend exception');
}
function Host({existing}){const [visible,setVisible]=useState(true);return visible?h(ui.ApplicationForm,{companyName:'Synthetic',record:existing?record:undefined,onSaved:()=>setVisible(false),onBusyChange:v=>busyEvents.push(v)}):h('p',null,'Saved');}
try {
 globalThis.fetch=async(url,options={})=>{
  if(url==='/api/applications/create-context')return Response.json({creationContext:'ac1.'+'A'.repeat(43)});
  if(!options.method)return Response.json({records:[record],entities:[{name:'Synthetic',entityType:'company'}],vacancies:[]});
  requests++;sent={method:options.method,body:JSON.parse(options.body)};
  await new Promise(resolve=>release=resolve);
  return Response.json(failed?{error:'Synthetic save failure'}:{records:[sent.body]},{status:failed?400:200});
 };
 for(const existing of [false,true]){
  await mount(Host,{existing});await type(root.root.findByType('textarea'),'Submitted draft');
  const hcm=()=>root.root.findAllByType('label').find(n=>n.children.includes('HCM-система')).findByType('input');
  await act(async()=>hcm().props.onChange({target:{checked:true}}));assert.equal(hcm().props.checked,true);
  const submit=root.root.findByType('form').props.onSubmit;
  // Both invocations deliberately use the same closure, before a new render.
  await act(async()=>{pending=submit({preventDefault(){}});void submit({preventDefault(){}});});
  assert.equal(requests,1);assertLocked();assert.equal(root.root.findByType('textarea').props.value,'Submitted draft');
  await act(async()=>{release();await pending;});
  assert.equal(sent.method,existing?'PUT':'POST');assert.equal(sent.body.comment,'Submitted draft');assert.deepEqual(sent.body.applicationSources,['hcm']);assert.equal(root.root.findAllByType('textarea').length,0);assert.deepEqual(busyEvents,[true,false]);await stop();
 }
 // A failed response keeps the form and draft; another user edit/retry is possible.
 await mount(Host,{existing:true});await type(root.root.findByType('textarea'),'Keep after error');
 await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});assertLocked();failed=true;
 await act(async()=>{release();await pending;});assert.equal(disabled(root.root.findByType('textarea')),false);assert.equal(root.root.findByType('textarea').props.value,'Keep after error');assert.match(JSON.stringify(root.toJSON()),/Synthetic save failure/);
 await type(root.root.findByType('textarea'),'Retry draft');failed=false;await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});assertLocked();await act(async()=>{release();await pending;});assert.equal(sent.body.comment,'Retry draft');assert.deepEqual(busyEvents,[true,false,true,false]);await stop();
 // Parent company selectors cannot unmount or replace the saving form.
 await mount(ui.CompanyApplications,{companyName:'Synthetic'});await act(async()=>root.root.findAllByType('button').find(n=>String(n.props.className).includes('company-application-card')).props.onClick());
 await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});assertLocked();
 for(const name of ['company-application-new','company-application-card active'])assert.equal(root.root.findByProps({className:name}).props.disabled,true);assert.equal(root.root.findByProps({title:'Удалить отклик'}).props.disabled,true);
 failed=true;await act(async()=>{release();await pending;});assert.equal(root.root.findByProps({className:'company-application-new'}).props.disabled,false);assert.equal(root.root.findByProps({title:'Удалить отклик'}).props.disabled,false);await stop();
 for(const kind of ['comment','contact','status']){
  await mount(ui.Applications,{nav});
  if(kind==='status')await act(async()=>{pending=root.root.findByProps({className:'application-status'}).props.onChange({target:{value:'Офер'}});});
  else {
   await act(async()=>kind==='contact'?root.root.findByProps({'aria-label':'Редактировать контакты рекрутера'}).props.onClick():root.root.findByProps({className:'application-comment rendered-md'}).props.onClick());
   await act(async()=>{pending=root.root.findByType('form').props.onSubmit({preventDefault(){}});});
  }
  assertLocked();assert.equal(root.root.findByProps({className:'application-delete'}).props.disabled,true);
  failed=true;await act(async()=>{release();await pending;});assert.equal(root.root.findByProps({className:'application-delete'}).props.disabled,false);
  const input=kind==='comment'?root.root.findByType('textarea'):kind==='contact'?root.root.findAllByType('input')[0]:root.root.findByProps({className:'application-status'});
  assert.equal(disabled(input),false);await stop();
 }
 console.log('Actual React R04: POST/PUT native input lock, same-render duplicate submission, success/error/retry lifecycle, parent selection/delete lock and inline comment/contact/status saving passed.');
} finally {await stop();globalThis.fetch=oldFetch;}
