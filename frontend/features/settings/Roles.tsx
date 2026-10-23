import {useRef,useState} from 'react';
import {mutate,useApi} from '../../api/client';
import {ErrorText,State} from '../../components/Common';
import type {VacancyRole,VacancyRoleCatalog} from '../../../shared/contracts';

function RoleRow({role,catalog,run}: {role:VacancyRole;catalog:VacancyRoleCatalog;run:(action:()=>Promise<unknown>)=>void}) {
  const [expanded,setExpanded]=useState(true);
  const blocked=new Set([role.id,...role.descendantIds]);
  const add=()=>{const name=prompt('Название дочерней роли');if(name?.trim())run(()=>mutate('/api/vacancy-roles','POST',{name,parentId:role.id}));};
  const rename=()=>{const name=prompt('Новое название роли',role.name);if(name?.trim()&&name.trim()!==role.name)run(()=>mutate('/api/vacancy-roles/'+role.id,'PUT',{name}));};
  const remove=()=>{if(confirm('Удалить роль «'+role.name+'»?'))run(()=>mutate('/api/vacancy-roles/'+role.id,'DELETE'));};
  return <div className="role-admin-node"><div className="role-admin-row">
    <div>{role.childCount>0?<button className="role-admin-toggle" type="button" aria-expanded={expanded} aria-controls={'role-children-'+role.id} onClick={()=>setExpanded(value=>!value)}><span aria-hidden="true">{expanded?'▾':'▸'}</span><b>{role.name}</b></button>:<b>{role.name}</b>}<small>{role.usageCount?'Вакансий: '+role.usageCount:role.isLeaf?'Не используется':'Категория'}</small></div>
    <div className="role-admin-actions"><button className="settings-button secondary" type="button" disabled={role.usageCount>0} onClick={add}>Добавить дочернюю</button><button className="settings-button secondary" type="button" onClick={rename}>Переименовать</button><button className="settings-button secondary" type="button" aria-label={'Поднять роль «'+role.name+'»'} onClick={()=>run(()=>mutate('/api/vacancy-roles/'+role.id,'PUT',{sortOrder:Math.max(0,role.sortOrder-11)}))}>↑</button><button className="settings-button secondary" type="button" aria-label={'Опустить роль «'+role.name+'»'} onClick={()=>run(()=>mutate('/api/vacancy-roles/'+role.id,'PUT',{sortOrder:role.sortOrder+11}))}>↓</button><button className="settings-button secondary" type="button" disabled={role.usageCount>0||role.childCount>0} title={role.usageCount?'Роль используется в вакансиях':role.childCount?'Сначала переместите или удалите дочерние роли':undefined} onClick={remove}>Удалить</button></div>
  </div><label className="role-admin-parent">Родитель<select className="settings-select" aria-label={'Родитель роли «'+role.name+'»'} value={role.parentId??''} onChange={event=>run(()=>mutate('/api/vacancy-roles/'+role.id,'PUT',{parentId:event.target.value?Number(event.target.value):null}))}><option value="">Корневой уровень</option>{catalog.roles.filter(item=>!blocked.has(item.id)&&!item.usageCount).map(item=><option key={item.id} value={item.id}>{item.path}</option>)}</select></label>
  {role.childCount>0&&<div className="role-admin-children" id={'role-children-'+role.id} hidden={!expanded}>{role.children?.map(child=><RoleRow key={child.id} role={child} catalog={catalog} run={run}/>)}</div>}</div>;
}

export default function Roles() {
  const query=useApi<VacancyRoleCatalog>('/api/vacancy-roles'),[busy,setBusy]=useState(false),[error,setError]=useState<unknown>();
  const pending=useRef(false);
  const run=(action:()=>Promise<unknown>)=>{if(pending.current)return;pending.current=true;setBusy(true);setError(undefined);void action().catch(setError).finally(()=>{pending.current=false;setBusy(false);});};
  const addRoot=()=>{const name=prompt('Название корневой категории');if(name?.trim())run(()=>mutate('/api/vacancy-roles','POST',{name,parentId:null}));};
  return <div className="settings-grid"><div className="settings-group-heading"><h2>Роли вакансий</h2><p>Иерархический справочник. В вакансии выбираются только конечные роли; категории используются в фильтрах.</p></div><article className="settings-card role-admin-card" aria-busy={busy}><fieldset className="role-admin-controls" disabled={busy}><div className="settings-actions"><button className="settings-button" type="button" disabled={busy} onClick={addRoot}>Добавить корневую категорию</button></div><ErrorText error={error}/><State loading={!query.data&&query.loading} error={query.error}>{query.data&&<div className="role-admin-tree">{query.data.tree.map(role=><RoleRow key={role.id} role={role} catalog={query.data!} run={run}/>)}</div>}</State></fieldset></article></div>;
}
