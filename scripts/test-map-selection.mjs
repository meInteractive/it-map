import assert from 'node:assert/strict';
import {createElement as h} from 'react';
import {act,create} from 'react-test-renderer';
import maplibregl from 'maplibre-gl';
import {moduleFor} from './test-support.mjs';
const maps=[];
class FakeMap {
 constructor(){this.handlers=new Map();this.sources=new Map();this.layers=new Map();this.commands=[];this.resizeCount=0;maps.push(this)}
 on(event,layer,callback){this.handlers.set(event+':'+(callback?layer:''),callback||layer)}
 addSource(id,source){this.sources.set(id,{data:source.data,updates:0,setData(data){this.data=data;this.updates++}})}
 addLayer(layer){this.layers.set(layer.id,layer)}
 getSource(id){return this.sources.get(id)}
 setLayoutProperty(id,key,value){const layer=this.layers.get(id);layer.layout??={};layer.layout[key]=value;}
 resize(){assert.ok(!this.removed,'resize on removed map');this.resizeCount++;}
 zoomIn(){this.commands.push(['in']);}
 zoomOut(){this.commands.push(['out']);}
 easeTo(options){this.commands.push(['ease',options]);}
 remove(){this.removed=true}
}
const originalMap=maplibregl.Map,originalFetch=fetch;
maplibregl.Map=FakeMap;
const ui=await moduleFor('scripts/react-test-entry.tsx');
const geometry=await moduleFor('frontend/features/map/geometry.ts');
const companies=[{name:'Alpha',entityType:'company',rank:1,offices:[{lat:55.75,lon:37.61},{lat:55.76,lon:37.62}]},{name:'Beta',entityType:'company',rank:2,offices:[{lat:55.75,lon:37.61}]}];
assert.deepEqual(geometry.officeFeatures(companies,'Alpha').features.map(f=>f.properties.selected),[true,true,false]);
assert.deepEqual(geometry.officeFeatures(companies).features.map(f=>f.properties.label),['№ 1 Alpha','№ 1 Alpha','№ 2 Beta']);
const unranked=[null,0,-1,1.5,Infinity,undefined].map((rank,i)=>({name:'Unranked '+i,rank,offices:[{lat:55.75,lon:37.61}]}));
assert.deepEqual(geometry.officeFeatures(unranked).features.map(f=>f.properties.label),unranked.map(c=>c.name));
assert.deepEqual(geometry.officeFeatures(companies).features.map(f=>f.properties.selected),[false,false,false]);
assert.deepEqual(geometry.officeFeatures(companies,'Alpha').features.map(f=>f.geometry),geometry.officeFeatures(companies).features.map(f=>f.geometry),'selection preserves office positions, including coincident offices');
globalThis.IS_REACT_ACT_ENVIRONMENT=true;
globalThis.window={addEventListener(){},removeEventListener(){},matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};
globalThis.document={addEventListener(){},removeEventListener(){},activeElement:null,createElement:()=>({getContext:()=>null})};
globalThis.localStorage={getItem:()=>null,setItem(){}};
globalThis.location={pathname:'/'};
globalThis.confirm=()=>true;
const observers=[],frames=new Map();let nextFrame=0;
globalThis.ResizeObserver=class{constructor(callback){this.callback=callback;observers.push(this);}observe(node){this.node=node;}disconnect(){this.disconnected=true;}};
globalThis.requestAnimationFrame=fn=>{frames.set(++nextFrame,fn);return nextFrame};
globalThis.cancelAnimationFrame=id=>frames.delete(id);
let root,resolveMap;const calls=[];
const label=n=>typeof n==='string'?n:Array.isArray(n)?n.map(label).join(''):n?.children?label(n.children):'';
const flush=()=>act(async()=>{await new Promise(r=>setTimeout(r,0))});
const buttons=()=>root.root.findAllByType('button');
const click=async b=>{assert.ok(b);await act(async()=>b.props.onClick());await flush()};
const selected=()=>maps[0].getSource('companies').data.features.filter(f=>f.properties.selected).map(f=>f.properties.companyName);
try{
 globalThis.fetch=async url=>{calls.push(url);
  if(url==='/api/session')return Response.json({name:'Candidate',isAdmin:false});
  if(url==='/api/tasks')return Response.json({tasks:[],entities:[]});
  if(url==='/api/google-calendar/status')return Response.json({configured:false});
  if(url==='/api/companies/map')return new Promise(resolve=>{resolveMap=resolve;});
  if(url.startsWith('/api/companies/detail?'))return Response.json({company:companies.find(c=>c.name===new URL(url,'https://example.test').searchParams.get('name'))});
  throw Error('Unexpected API '+url);
 };
 await act(async()=>{root=create(h(ui.Application),{createNodeMock:e=>e.props.id==='maplibreMap'?{}:null})});await flush();
 assert.ok(!calls.includes('/api/companies/map'));
 await click(buttons().find(b=>label(b)==='Карта'));
 assert.equal(maps.length,1);assert.match(label(root.toJSON()),/Загрузка карты…/);
 await act(async()=>resolveMap(Response.json({companies})));await flush();assert.doesNotMatch(label(root.toJSON()),/Загрузка карты…/);
 const initialFrame=frames.values().next().value;frames.clear();initialFrame();observers[0].callback();assert.equal(maps[0].resizeCount,2);
 await act(async()=>maps[0].handlers.get('load:')());await flush();
 const visible=id=>maps[0].layers.get(id).layout.visibility;
 const metroButton=()=>buttons().find(b=>b.props['aria-label']==='Показать или скрыть станции метро');
 const linesButton=()=>buttons().find(b=>b.props['aria-label']==='Показать или скрыть линии метро');
 assert.equal(metroButton().props['aria-pressed'],true);assert.equal(linesButton().props['aria-pressed'],true);
 await click(metroButton());assert.equal(metroButton().props['aria-pressed'],false);for(const id of ['metro-dots','metro-label'])assert.equal(visible(id),'none');for(const id of ['metro-outline','metro-line'])assert.equal(visible(id),'visible');
 await click(linesButton());assert.equal(linesButton().props['aria-pressed'],false);for(const id of ['metro-outline','metro-line'])assert.equal(visible(id),'none');
 await click(metroButton());for(const id of ['metro-dots','metro-label'])assert.equal(visible(id),'visible');assert.equal(visible('metro-line'),'none');
 await click(linesButton());for(const id of ['metro-outline','metro-line'])assert.equal(visible(id),'visible');
 await click(buttons().find(b=>b.props['aria-label']==='Приблизить'));await click(buttons().find(b=>b.props['aria-label']==='Отдалить'));await click(buttons().find(b=>b.props['aria-label']==='Показать всю Москву'));
 assert.deepEqual(maps[0].commands,[['in'],['out'],['ease',{center:[37.6176,55.7558],zoom:10.7,bearing:0,pitch:0,duration:260}]]);
 assert.deepEqual(selected(),[]);
 assert.deepEqual(maps[0].getSource('companies').data.features.map(f=>f.properties.label),['№ 1 Alpha','№ 1 Alpha','№ 2 Beta']);
 const paint=maps[0].layers.get('company-dots').paint;
 assert.deepEqual(paint['circle-radius'],['case',['get','selected'],8,6]);
 assert.deepEqual(paint['circle-stroke-width'],['case',['get','selected'],3,2]);
 const metroUpdates=maps[0].getSource('metro').updates;
 // Реальная строка поиска меняет GeoJSON, а не только UI-счётчик.
 const searchField=root.root.find(n=>n.type?.name==='SectionSearch');
 await act(async()=>searchField.props.onChange('Alpha'));await flush();
 assert.deepEqual(maps[0].getSource('companies').data.features.map(f=>f.properties.companyName),['Alpha','Alpha'],'Поиск исключает чужие маркеры');
 await act(async()=>searchField.props.onChange('Нет совпадений'));await flush();
 assert.deepEqual(maps[0].getSource('companies').data.features,[],'Нулевой поиск удаляет все старые маркеры');
 await act(async()=>searchField.props.onChange(''));await flush();
 assert.equal(maps[0].getSource('companies').data.features.length,3,'Очистка возвращает исходные офисы');
 assert.equal(calls.filter(url=>url==='/api/companies/map').length,1,'Поиск не вызывает новый GET карты');
 // Фильтры управляют реальными GeoJSON-маркерами без нового запроса карты.
 await click(buttons().find(b=>b.props.className==='rank-filter-button'));
 const favoriteLabel=root.root.findAllByType('label').find(node=>label(node)==='Избранные');
 assert.ok(favoriteLabel,'Фильтр избранного должен быть в действующем интерфейсе');
 const favoriteInput=()=>root.root.findAllByType('label').find(node=>label(node)==='Избранные').findByType('input');
 await act(async()=>favoriteInput().props.onChange({target:{checked:true}}));await flush();
 assert.deepEqual(maps[0].getSource('companies').data.features,[],'Пустое пересечение немедленно удаляет старые маркеры');
 assert.equal(calls.filter(url=>url==='/api/companies/map').length,1,'Фильтрация не делает новый запрос');
 await act(async()=>favoriteInput().props.onChange({target:{checked:false}}));await flush();
 assert.equal(maps[0].getSource('companies').data.features.length,3,'Снятие только избранного возвращает маркеры');
 const marker=async name=>{await act(async()=>maps[0].handlers.get('click:company-dots')({features:[{properties:{companyName:name}}]}));await flush()};
 await marker('Alpha');assert.deepEqual(selected(),['Alpha','Alpha']);
 await act(async()=>favoriteInput().props.onChange({target:{checked:true}}));await flush();
 assert.deepEqual(maps[0].getSource('companies').data.features,[],'Фильтр исключил выбранную компанию без подмены');
 await act(async()=>favoriteInput().props.onChange({target:{checked:false}}));await flush();
 assert.deepEqual(selected(),['Alpha','Alpha'],'Возврат фильтра выделяет прежнюю компанию, а не другую');
 assert.equal(calls.filter(url=>url==='/api/companies/map').length,1);
 await marker('Beta');assert.deepEqual(selected(),['Beta']);
 await click(buttons().find(b=>b.props['aria-label']==='Закрыть карточку'));assert.deepEqual(selected(),[]);
 await marker('Alpha');
 const section=root.root.find(n=>n.type?.name==='MapSection');
 await act(async()=>section.props.nav.openCompany('Alpha','agency'));await flush();
 assert.deepEqual(selected(),[],'agency with the same name does not select company offices');
 assert.equal(maps.length,1,'selection does not recreate map');
 assert.equal(maps[0].getSource('metro').updates,metroUpdates,'selection does not recalculate metro');
 assert.equal(calls.filter(u=>u==='/api/companies/map').length,1,'selection uses cached map projection');
 await act(async()=>root.unmount());root=null;assert.equal(maps[0].removed,true);
 assert.equal(observers[0].disconnected,true);
 const component=await moduleFor('frontend/features/map/Map.tsx');
 const filters={rankTop:true,rankBottom:true,rank101to150:true,rank151to200:true,rank201to250:true,rank251to300:true,rankBeyond300:true,rankUnranked:true,favoriteOnly:false,showHidden:false,vacanciesOnly:false,applicationsOnly:false};
 globalThis.fetch=url=>{calls.push(url);return new Promise(resolve=>{resolveMap=resolve;});};
 await act(async()=>{root=create(h(component.default,{nav:{openCompany(){}},filters,query:'',onCount(){} }),{createNodeMock:e=>e.props.id==='maplibreMap'?{}:null});});await flush();
 assert.match(label(root.toJSON()),/Загрузка карты…/);
 await act(async()=>resolveMap(Response.json({error:'Synthetic map API failure'},{status:503})));await flush();
 assert.match(label(root.toJSON()),/Synthetic map API failure/);assert.equal(root.root.findAll(n=>n.props.role==='alert').length,1);
 const pendingResize=frames.values().next().value;assert.equal(typeof pendingResize,'function');
 await act(async()=>root.unmount());root=null;assert.equal(maps[1].removed,true);assert.equal(observers[1].disconnected,true);
 assert.doesNotThrow(()=>pendingResize(),'a queued animation callback after unmount must not resize the removed map');
 assert.doesNotThrow(()=>observers[1].callback(),'a delivered observer callback after disconnect must be ignored');
 assert.equal(frames.size,0,'unmount cancels the pending animation frame');
 console.log('Map selection: Application → marker click → all company offices, switch/close/agency guard, unchanged geometry, loading/API error, independent toggles, zoom/reset, resize/dispose and no reload/recreation/metro recomputation passed. WebGL appearance not tested.');
}finally{if(root)await act(async()=>root.unmount());ui.clearCache();maplibregl.Map=originalMap;globalThis.fetch=originalFetch;}
