import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act, create} from 'react-test-renderer';
import {moduleFor} from './test-support.mjs';

// Regression: online must revalidate the private session after an offline event.
// Rendering the real Application catches missing imports in an event handler;
// Vite transpilation alone does not check undefined runtime identifiers.
const old = Object.fromEntries(['window','document','navigator','location','history','fetch','localStorage','sessionStorage']
  .map(key => [key,Object.getOwnPropertyDescriptor(globalThis,key)]));
const events = new Map(), calls = [];
let root;
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
const addEventListener = (name, callback) => {
  if (!events.has(name)) events.set(name,new Set());
  events.get(name).add(callback);
};
const removeEventListener = (name,callback) => events.get(name)?.delete(callback);
const dispatchEvent = event => {
  for(const callback of events.get(event.type) || []) callback(event);
  return true;
};
const setGlobal = (key,value) => Object.defineProperty(globalThis,key,
  {configurable:true,writable:true,value});
const matchMedia = () => ({matches:false,addEventListener(){},removeEventListener(){}});
const flush = async () => act(async () => { await new Promise(r => setTimeout(r,0)); });
try {
  const documentMock={addEventListener(){},removeEventListener(){},visibilityState:'visible',
    documentElement:{style:{}},createElement(){return {style:{},getContext(){return null;}};},
    querySelector(){return null;}};
  setGlobal('document',documentMock);
  setGlobal('window',{addEventListener,removeEventListener,dispatchEvent,matchMedia,document:documentMock,URL:{createObjectURL:()=> 'blob:synthetic-ci-map-worker'}});
  setGlobal('navigator',{onLine:true});
  setGlobal('location',{pathname:'/',protocol:'http:'});
  setGlobal('history',{state:null,pushState(){},replaceState(){},back(){}});
  setGlobal('sessionStorage',{getItem(){return null;},removeItem(){},setItem(){}});
  setGlobal('localStorage',{getItem(){return null;},setItem(){}});
  setGlobal('fetch', async url => {
    calls.push(url);
    if(url === '/api/session') return Response.json({name:'Disposable CI',isAdmin:false});
    if(url === '/api/tasks') return Response.json({tasks:[],entities:[]});
    if(url === '/api/google-calendar/status') return Response.json({configured:false});
    return Response.json({});
  });
  const {default:Application} = await moduleFor('frontend/Application.tsx');
  await act(async () => { root=create(h(Application)); });
  await flush();
  const sessions = () => calls.filter(url => url === '/api/session').length;
  assert.equal(sessions(),1,'initial online state verifies the session');
  await act(async () => dispatchEvent(new Event('offline')));
  await flush();
  assert.match(JSON.stringify(root.toJSON()),/Нет подключения к сети/,'private workspace is hidden offline');
  await act(async () => dispatchEvent(new Event('online')));
  await flush();
  assert.ok(sessions() >= 2,'online revalidates /api/session without undefined references');
  assert.doesNotMatch(JSON.stringify(root.toJSON()),/Нет подключения к сети/,
    'successful session response restores the online workspace');
  console.log('PASS: Application offline hides private workspace; online revalidates /api/session and recovers');
} finally {
  if(root) await act(async () => root.unmount());
  for(const [key,descriptor] of Object.entries(old)) {
    if(descriptor) Object.defineProperty(globalThis,key,descriptor);
    else delete globalThis[key];
  }
}
