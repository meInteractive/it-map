import ApifySettings from './Apify';
import Scheduler from './Scheduler';
import '../../styles/parser.css';
import FirecrawlSettings from './Firecrawl';
import ParsingTools from './ParsingTools';
import {useState} from 'react';
import {useApi,mutate} from '../../api/client';
import {ActionForm,State,dateLabel} from '../../components/Common';
import {MarkdownEditor} from '../../components/Markdown';
import {useDirty} from '../../hooks/useDirty';
import {onCacheEvent} from '../../api/client';
import {useEffect,useRef} from 'react';

export interface ClassificationRole {roleId:number;name:string;markdown:string;revision:number;updatedAt:string;enabled:boolean;defaultMarkdown:string|null;descendants:{id:number;path:string}[];}
export interface ClassificationSettings {maxLength:number;roles:ClassificationRole[];}
export interface ClassificationResult {title:string;model:string|null;status:'complete'|'incomplete'|'no_active_roles';accepted:boolean|null;durationMs:number;requestDurationMs:number|null;costRub:number|null;rubPerUsd:number;results:{roleId:number;name:string;promptRevision:number;decision:'match'|'reject'|null;error:{code:string;message:string}|null}[];}

function ClassificationPrompt({initial,limit}: {initial:ClassificationRole;limit:number}) {
 const [value,setValue]=useState(initial.markdown),[saved,setSaved]=useState(initial.markdown),[revision,setRevision]=useState(initial.revision),[updated,setUpdated]=useState(initial.updatedAt),[message,setMessage]=useState('');
 useDirty(value!==saved);
 const title='Классификация вакансий: '+initial.name;
 return <article className="resume-card"><h3>{title}</h3><p className="resume-hint">Общий промт корневой роли. Используется JEV только для проверки названия вакансии.</p><ActionForm className="resume-form" onSubmit={async()=>{
  const result=await mutate<ClassificationRole>('/api/classification/prompts/'+initial.roleId,'PUT',{markdown:value,revision});
  setSaved(result.markdown);setRevision(result.revision);setUpdated(result.updatedAt);setMessage('Сохранено.');
 }}>{busy=><><fieldset disabled={busy} className="classification-fieldset"><MarkdownEditor label={title} value={value} onChange={v=>{setValue(v);setMessage('');}} limit={limit} className="resume-editor" footer={<button className="settings-button" type="submit" disabled={busy||value===saved||Array.from(value).length>limit}>Сохранить</button>}/>{initial.defaultMarkdown!==null&&<button className="settings-button secondary" type="button" onClick={()=>{setValue(initial.defaultMarkdown!);setMessage('Стандартный промт восстановлен в редакторе. Нажмите «Сохранить».');}}>Восстановить стандартный промт</button>}</fieldset><p className="resume-status" role="status">{message||(value!==saved?'Есть несохранённые изменения.':'Изменений нет.')}</p><p className="resume-status">Версия: {revision}. Обновлено: {dateLabel(updated)}</p></>}</ActionForm></article>;
}

export function ClassificationPrompts(){
 const q=useApi<ClassificationSettings>('/api/classification/settings');
 return <section><h2>Классификация вакансий</h2>{q.error&&<button className="settings-button secondary" onClick={q.reload}>Повторить загрузку</button>}<State loading={!q.data&&q.loading} error={q.error}>{q.data&&<div className="prompts-grid">{q.data.roles.map(role=><ClassificationPrompt key={role.roleId} initial={role} limit={q.data!.maxLength}/>)}{!q.data.roles.length&&<p>Создайте корневую роль в справочнике ролей.</p>}</div>}</State></section>;
}

function ParsingRole({role}: {role:ClassificationRole}) {
 return <article className="settings-card"><h3>{role.name}</h3><ActionForm onSubmit={async()=>{await mutate('/api/classification/roles/'+role.roleId,'PUT',{enabled:!role.enabled,revision:role.revision});}}>{busy=><label><input type="checkbox" aria-label={'Парсинг включён: '+role.name} checked={role.enabled} disabled={busy} onChange={event=>{event.currentTarget.form?.requestSubmit();}}/> Парсинг включён</label>}</ActionForm><p>Промт классификации: {role.markdown.trim()?'настроен':'не настроен, направление пропускается'}.</p>{role.descendants.length>0&&<><details><summary>Дочерние роли ({role.descendants.length})</summary><ul>{role.descendants.map(child=><li key={child.id}>{child.path}</li>)}</ul></details></>}</article>;
}

export function ParsingSettings({onNavigate}:{onNavigate?:(tab:string)=>void}={}){
 const q=useApi<ClassificationSettings>('/api/classification/settings');
 return <section className="parser-settings"><Scheduler onSettings={onNavigate} roles={<section className="parser-role-settings"><h2>Направления отбора</h2><p className="resume-hint">Включённые корневые роли с сохранённым промтом участвуют в классификации заголовков. Настройки общие для всех пользователей.</p><State loading={!q.data&&q.loading} error={q.error}>{q.data&&<div className="settings-grid">{q.data.roles.map(role=><ParsingRole key={role.roleId} role={role}/>)}{!q.data.roles.length&&<p>Создайте корневую роль в справочнике.</p>}</div>}</State>{onNavigate&&<button className="settings-button secondary" type="button" onClick={()=>onNavigate('prompts')}>Открыть промты обработки</button>}</section>} services={<><ApifySettings/><FirecrawlSettings/></>} tools={(tool,onChange)=><ParsingTools tool={tool} onChange={onChange} enabled={!!q.data?.roles.some(role=>role.enabled&&!!role.markdown.trim())}/>}/></section>;
}

export function ClassificationResultView({result}:{result:ClassificationResult}) {
 return <div className="classification-result" aria-live="polite"><h4>Название: {result.title}</h4>{result.status==='no_active_roles'?<p>Нет активных корневых ролей с промтом. Запрос в JEV не выполнялся.</p>:<><div className="classification-table-scroll"><table className="classification-table"><thead><tr><th>Корневая роль</th><th>Результат</th><th>Версия промта</th></tr></thead><tbody>{result.results.map(row=><tr key={row.roleId}><td>{row.name}</td><td>{row.error?<span className="error">Техническая ошибка: {row.error.message}</span>:row.decision==='match'?'Подходит':row.decision==='reject'?'Не подходит':'Решение не получено'}</td><td>{row.promptRevision}</td></tr>)}</tbody></table></div><p>{result.accepted===true?'Найдено подходящее направление.':result.accepted===false?'Ни одно проверенное направление не подходит.':'Классификация не завершена. Повторите проверку.'}</p><p>Модель: {result.model||'Ответ модели не получен'}. Общее время: {(result.durationMs/1000).toLocaleString('ru-RU')} с.{result.requestDurationMs!==null&&<> Один запрос для всех направлений: {(result.requestDurationMs/1000).toLocaleString('ru-RU')} с.</>}</p><p>{result.costRub===null?'Стоимость: провайдер не сообщил достоверную сумму.':<>Стоимость: {result.costRub.toLocaleString('ru-RU',{maximumFractionDigits:6})} ₽. Курс: {result.rubPerUsd} ₽ за $1.</>}</p></>}</div>;
}

export function ClassificationTest({enabled}:{enabled:boolean}) {
 const [title,setTitle]=useState(''),[result,setResult]=useState<ClassificationResult>();
 const epoch=useRef(0);
 useEffect(()=>{const unsubscribe=onCacheEvent(event=>{if(event.type==='reset'){epoch.current++;setResult(undefined);}});return()=>{epoch.current++;unsubscribe();};},[]);
 return <article className="settings-card"><h3>Проверка классификации</h3><p>Введите только название вакансии. Проверка использует сохранённые промты и не изменяет вакансии.</p>{!enabled&&<p>Нет активных корневых ролей с промтом. Включите направление и сохраните его промт.</p>}<ActionForm lockWhileBusy disabled={!enabled} onSubmit={async()=>{const generation=epoch.current;setResult(undefined);const response=await mutate<ClassificationResult>('/api/classification/test','POST',{title});if(generation===epoch.current)setResult(response);}}>{busy=><><label className="task-form-field"><span>Название вакансии</span><input className="settings-input" aria-label="Название вакансии для классификации" value={title} onChange={event=>{setTitle(event.target.value);setResult(undefined);}} required/></label><p className="resume-hint">{Array.from(title).length} / 500 символов</p><button type="submit" className="settings-button" disabled={busy||!title.trim()||Array.from(title).length>500}>{busy?'Проверяем…':'Проверить'}</button></>}</ActionForm>{result&&<ClassificationResultView result={result}/>}</article>;
}
