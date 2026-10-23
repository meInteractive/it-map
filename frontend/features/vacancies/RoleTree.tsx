import {useApi} from '../../api/client';
import type {VacancyRole,VacancyRoleCatalog} from '../../../shared/contracts';

function RoleNodes({nodes=[],selected,onChange,leafOnly=false,depth=0}: {
  nodes: VacancyRole[];
  selected: number[];
  onChange: (ids:number[])=>void;
  leafOnly?: boolean;
  depth?: number;
}) {
  return <div className="role-tree-level" data-depth={depth}>{nodes.map(node => {
    const checked=selected.includes(node.id), disabled=leafOnly&&!node.isLeaf;
    const line=<label className={'role-tree-option'+(disabled?' category':'')}>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={event=>onChange(event.target.checked?[...selected,node.id]:selected.filter(id=>id!==node.id))}/>
      <span>{node.name}</span>{!node.isLeaf&&<small>{node.descendantIds.length}</small>}
    </label>;
    return <div className="role-tree-node" key={node.id}>{node.children?.length
      ? <details open={depth===0}><summary>{line}</summary><RoleNodes nodes={node.children} selected={selected} onChange={onChange} leafOnly={leafOnly} depth={depth+1}/></details>
      : line}</div>;
  })}</div>;
}

export function RoleTreeOptions({selected,onChange,leafOnly=false}: {
  selected:number[];
  onChange:(ids:number[])=>void;
  leafOnly?:boolean;
}) {
  const query=useApi<VacancyRoleCatalog>('/api/vacancy-roles');
  const tree=Array.isArray(query.data?.tree)?query.data.tree:[];
  if(query.error)return <div className="error">Не удалось загрузить роли.</div>;
  if(!query.data)return <div className="vacancy-editor-hint">Загрузка ролей…</div>;
  return <RoleNodes nodes={tree} selected={selected} onChange={onChange} leafOnly={leafOnly}/>;
}

export function VacancyRolePicker({selected,onChange}: {selected:number[];onChange:(ids:number[])=>void}) {
  const query=useApi<VacancyRoleCatalog>('/api/vacancy-roles');
  const leaves=Array.isArray(query.data?.leaves)?query.data.leaves:[];
  const labels=leaves.filter(role=>selected.includes(role.id)).map(role=>role.path);
  return <div className="vacancy-role-picker">
    <div className="vacancy-role-selection" aria-live="polite">{labels.length?labels.map(label=><span key={label}>{label}</span>):'Роли не выбраны'}</div>
    <div className="vacancy-role-tree-box"><RoleTreeOptions selected={selected} onChange={onChange} leafOnly/></div>
  </div>;
}

export function vacancyRoleText(vacancy:{role?:string;roles?:VacancyRole[]},paths=true) {
  return vacancy.roles?.length ? vacancy.roles.map(role=>paths?role.path:role.name).join('; ') : vacancy.role||'';
}
