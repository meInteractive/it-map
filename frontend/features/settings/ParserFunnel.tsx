import {useId,useRef,useState,type Dispatch,type SetStateAction} from 'react';
import {useApi} from '../../api/client';
import {State} from '../../components/Common';
import {funnelStages,type FunnelStage,type ParserFunnelReport} from '../../../shared/parser-funnel';

const shortDate=(day:string)=>day.slice(8,10)+'.'+day.slice(5,7);
const number=(n:number)=>n.toLocaleString('ru-RU',{maximumFractionDigits:1});
export function FunnelView({report,selected:controlled,onSelectedChange}:{report:ParserFunnelReport;selected?:FunnelStage[];onSelectedChange?:Dispatch<SetStateAction<FunnelStage[]>>}){
 const [ownSelection,setOwnSelection]=useState<FunnelStage[]>(['received','published']),id=useId(),selected=controlled??ownSelection,setSelected=onSelectedChange??setOwnSelection;
 const {daily,cohort,averages,totals}=report,visible=funnelStages.filter(s=>selected.includes(s.id));
 const width=920,left=40,right=18,top=16,bottom=184,tick=Math.max(1,Math.ceil(Math.max(1,...daily.flatMap(p=>visible.map(s=>p[s.id])))/4));
 const x=(i:number)=>left+i*(width-left-right)/Math.max(1,daily.length-1),y=(n:number)=>bottom-n/(tick*4)*(bottom-top);
 return <>
  <div className="parser-funnel-totals">{(['received','published'] as const).map(key=><div key={key}><span>{key==='received'?'Получено за 30 дней':'Опубликовано за 30 дней'}</span><strong>{number(totals[key])}</strong><span>Среднее: {number(averages[key])} в день</span></div>)}</div>
  <h3>Динамика по датам этапов</h3><p className="resume-hint">{shortDate(report.from)}–{shortDate(report.to)}.{report.to.slice(0,4)}, по Москве. Каждая публикация учитывается один раз на этапе. Текущий день неполный.</p>
  <fieldset className="parser-funnel-series"><legend>Этапы на графике</legend>{funnelStages.map(s=><label key={s.id}><input type="checkbox" checked={selected.includes(s.id)} onChange={()=>setSelected(current=>current.includes(s.id)?current.filter(k=>k!==s.id):[...current,s.id])}/><i style={{background:s.color}}/>{s.title}</label>)}</fieldset>
  <p className="resume-hint">Сплошная линия — число вакансий, пунктир того же цвета — среднее за 30 дней.</p>
  {visible.length?<div className="parser-history-scroll"><svg className="parser-history-chart" viewBox={'0 0 '+width+' 224'} role="img" aria-labelledby={id+'-title '+id+'-desc'}>
   <title id={id+'-title'}>Количество обработанных вакансий за 30 дней</title><desc id={id+'-desc'}>Выбранные этапы и их средние по календарным дням. Точные значения доступны в таблице под графиком.</desc>
   {[0,1,2,3,4].map(i=><g key={i}><line x1={left} x2={width-right} y1={y(i*tick)} y2={y(i*tick)} className="parser-history-grid"/><text x={left-8} y={y(i*tick)+4} textAnchor="end">{i*tick}</text></g>)}
   {daily.map((point,i)=>(i%5===0||i===daily.length-1)&&<text key={point.day} x={x(i)} y={bottom+23} textAnchor="middle">{shortDate(point.day)}</text>)}
   {visible.map(s=><g key={s.id}><polyline fill="none" stroke={s.color} strokeWidth="2.5" points={daily.map((p,i)=>x(i)+','+y(p[s.id])).join(' ')}/><line x1={left} x2={width-right} y1={y(averages[s.id])} y2={y(averages[s.id])} stroke={s.color} strokeDasharray="6 4" opacity=".65"/>{daily.map((p,i)=><circle key={p.day} cx={x(i)} cy={y(p[s.id])} r="2.5" fill={s.color}><title>{shortDate(p.day)+' · '+s.title+': '+p[s.id]}</title></circle>)}</g>)}
  </svg></div>:<p className="parser-empty">Выберите хотя бы один этап для графика.</p>}
  <div className="parser-line"><h3>Воронка поступивших за 30 дней</h3><span className="resume-hint">Всего: {number(cohort.received)}</span></div>
  <p className="resume-hint">Этапы, которых достигли эти вакансии к текущему моменту. Динамика выше может включать публикацию вакансий, полученных раньше.</p>
  <div className="parser-funnel-stages">{funnelStages.map(s=><div className="parser-funnel-stage" key={s.id}><div className="parser-line"><span>{s.title}</span><strong>{number(cohort[s.id])}<small>{cohort.received?' · '+number(cohort[s.id]/cohort.received*100)+'%':''}</small></strong></div><div className="parser-funnel-track"><div style={{width:(cohort.received?cohort[s.id]/cohort.received*100:0)+'%',background:s.color}}/></div></div>)}</div>
  <div className="parser-funnel-states">{([{key:'rejected',label:'Отклонено отбором'},{key:'waiting',label:'Ожидает обработки'},{key:'error',label:'Требует повтора / ошибка'}] as const).map(s=><div key={s.key} className={s.key}><strong>{number(cohort[s.key])}</strong><span>{s.label}</span></div>)}</div>
  <p>Из опубликованных: новых карточек — <strong>{number(cohort.created)}</strong>, привязано к существующим — <strong>{number(cohort.linked)}</strong>.</p>
  <p className="resume-hint">Единица счёта — публикация источника. Повторные попытки не добавляют вакансии. Этапы пересекаются: их не складывают. Apify может вернуть полный текст ещё до отбора.</p>
  {cohort.undatedPublished>0&&<p className="resume-hint">У {cohort.undatedPublished} ранее опубликованных записей нет достоверной даты импорта: они учтены в воронке, но не в дневной динамике публикаций.</p>}
  <details><summary>Значения этапов по дням</summary><div className="docs-table-wrap"><table className="parser-history-table"><thead><tr><th>Дата</th>{funnelStages.map(s=><th key={s.id}>{s.title}</th>)}</tr></thead><tbody>{daily.map(p=><tr key={p.day}><td>{shortDate(p.day)}.{p.day.slice(0,4)}</td>{funnelStages.map(s=><td key={s.id}>{p[s.id]}</td>)}</tr>)}</tbody></table></div></details>
 </>;
}
export default function ParserFunnel(){
 const [sourceId,setSourceId]=useState('all'),[parser,setParser]=useState('all');
 const [selected,setSelected]=useState<FunnelStage[]>(['received','published']),options=useRef<ParserFunnelReport|null>(null);
 const q=useApi<ParserFunnelReport>('/api/scheduler/funnel?sourceId='+encodeURIComponent(sourceId)+'&parser='+encodeURIComponent(parser),{refreshIntervalMs:30000});
 const report=q.data&&Array.isArray(q.data.daily)?q.data:undefined;
 if(report)options.current=report;
 return <article className="settings-card parser-funnel"><div className="parser-line"><h2>Воронка обработки вакансий</h2><button className="settings-button secondary" type="button" onClick={q.reload}>Обновить статистику</button></div>
  <div className="parser-funnel-filters"><label><span>Источник вакансий</span><select className="settings-select" aria-label="Источник воронки" value={sourceId} onChange={e=>setSourceId(e.target.value)}><option value="all">Все источники</option>{options.current?.sourceOptions.map(s=><option key={s.id} value={s.id}>{s.name}</option>)}</select></label><label><span>Парсер списка</span><select className="settings-select" aria-label="Парсер воронки" value={parser} onChange={e=>setParser(e.target.value)}><option value="all">Все парсеры</option>{options.current?.parserOptions.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</select></label></div>
  <p className="resume-hint">Источник и парсер выбираются независимо. «Не определён» — исторические записи без доказанного парсера получения списка. Ручной ввод и Excel сюда не входят.</p>
  <State loading={!report&&q.loading} error={q.error}>{report?<FunnelView report={report} selected={selected} onSelectedChange={setSelected}/>:!q.loading&&<p>Статистика пока не получена.</p>}</State>
 </article>;
}
