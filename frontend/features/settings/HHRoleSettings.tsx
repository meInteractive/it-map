import {useState} from 'react';
import {useApi,mutate} from '../../api/client';
import {ActionForm,State} from '../../components/Common';
import {useDirty} from '../../hooks/useDirty';

interface HHRole {id:string;name:string;}
interface Tree {categories:{id:string;name:string;roles:HHRole[]}[];}
interface Setting {roleId:number;name:string;revision:number;roleIds:string[];}
interface Settings {generation:string;roles:Setting[];}

function RolePicker({initial,tree,generation}:{initial:Setting;tree:Tree;generation:string}) {
 const [saved,setSaved]=useState(initial),[ids,setIDs]=useState(initial.roleIds),[message,setMessage]=useState('');
 const selected=new Set(ids),known=new Set(tree.categories.flatMap(category=>category.roles.map(role=>role.id)));
 const stale=ids.filter(id=>!known.has(id));
 const dirty=JSON.stringify([...ids].sort())!==JSON.stringify([...saved.roleIds].sort());
 useDirty(dirty);
 function toggle(id:string,checked:boolean){setIDs(current=>checked?Array.from(new Set([...current,id])):current.filter(value=>value!==id));setMessage('');}
 return <article className="settings-card"><h3>{initial.name}</h3><p>Выбрано ролей HH: {ids.length}. Эти роли применяются только к направлению «{initial.name}».</p>
 {stale.length>0&&<p role="alert">В текущем справочнике отсутствуют ранее выбранные ID: {stale.join(', ')}. Снимите их перед сохранением. {stale.map(id=><button key={id} type="button" className="settings-button secondary" onClick={()=>toggle(id,false)}>Удалить {id}</button>)}</p>}
 <ActionForm lockWhileBusy onSubmit={async()=>{const next=await mutate<Setting>('/api/hh/role-settings','PUT',{generation,roleId:initial.roleId,revision:saved.revision,roleIds:ids});setSaved(next);setIDs(next.roleIds);setMessage('Сохранено.');}}>{busy=><>
 {tree.categories.map(category=><details key={category.id}><summary>{category.name}</summary><div className="scheduler-stack">{category.roles.map(role=><label key={role.id}><input type="checkbox" checked={selected.has(role.id)} disabled={busy} onChange={event=>toggle(role.id,event.target.checked)}/> {role.name}</label>)}</div></details>)}
 <button type="submit" className="settings-button" disabled={busy||!dirty||stale.length>0}>{busy?'Сохраняем…':'Сохранить роли HH'}</button><p role="status">{message||(dirty?'Есть несохранённые изменения.':'')}</p></>}</ActionForm></article>;
}

export default function HHRoleSettings(){
 const settings=useApi<Settings>('/api/hh/role-settings'),tree=useApi<Tree>('/api/hh/professional-roles');
 return <section className="parser-role-settings"><h2>Профессиональные роли HH</h2><p className="resume-hint">Выберите роли HH отдельно для каждого направления. Настройки будут использоваться после подключения фильтра к парсеру. Изменение настроек не запускает сбор.</p>
 <button type="button" className="settings-button secondary" onClick={()=>{settings.reload();tree.reload();}}>Обновить список и настройки</button>
 <State loading={!settings.data&&settings.loading} error={settings.error}><State loading={!tree.data&&tree.loading} error={tree.error}>{settings.data&&tree.data&&<div className="settings-grid">{settings.data.roles.map(role=><RolePicker key={role.roleId+':'+role.revision+':'+settings.data!.generation} initial={role} tree={tree.data!} generation={settings.data!.generation}/>)}{settings.data.roles.length===0&&<p>Создайте крупную роль в справочнике.</p>}</div>}</State></State></section>;
}
