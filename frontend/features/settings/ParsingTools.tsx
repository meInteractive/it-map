import {useState,useEffect} from 'react';
import {Field} from '../../components/Common';
import {FirecrawlSourcePanel} from './Firecrawl';
import CandidateClassification from './CandidateClassification';
import VacancyDetails from './VacancyDetails';
import VacancyImport from './VacancyImport';
import {ClassificationTest} from './Classification';
const tools=[['source','Источник и правило извлечения'],['title','Проверка классификации заголовка'],['classification','Пакетная классификация'],['fulltext','Полный текст, уровень и структурирование'],['import','Импорт и возможные дубли']] as const;
export default function ParsingTools({tool,onChange,enabled}:{tool:string;onChange:(tool:string)=>void;enabled:boolean}){
 const [visited,setVisited]=useState<string[]>([]);const mounted=new Set([...visited,tool]);
 useEffect(()=>{setVisited(v=>v.includes(tool)?v:[...v,tool]);},[tool]);
 return <section className="parser-tools"><h2>Проверка и восстановление</h2><p className="resume-hint">Открытие инструмента не запускает сбор. Внешние запросы и повторная обработка выполняются отдельными кнопками; стоимость показывается соответствующим инструментом.</p><Field label="Что проверить"><select className="settings-select" value={tool} onChange={e=>{setVisited(v=>Array.from(new Set([...v,tool])));onChange(e.target.value);}}>{tools.map(([id,label])=><option key={id} value={id}>{label}</option>)}</select></Field>{tools.map(([id])=><div hidden={tool!==id} key={id}>{mounted.has(id)&&(id==='source'?<FirecrawlSourcePanel/>:id==='title'?<ClassificationTest enabled={enabled}/>:id==='classification'?<CandidateClassification/>:id==='fulltext'?<VacancyDetails/>:<VacancyImport/>)}</div>)}</section>;
}
