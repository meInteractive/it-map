import {useId} from 'react';

export interface ErrorHistory {
 timezone:string;metric:'daily_peak_active';days:number;observedDays:number;
 average:{p1:number|null;p2:number|null};
 points:{day:string;p1:number|null;p2:number|null;observedAt:number|null}[];
}
const priorities=[{key:'p1',title:'P1 · Критичные',color:'#b3263d'},{key:'p2',title:'P2 · Обработка',color:'#b97716'}] as const;
const value=(n:number|null)=>n===null?'—':n.toLocaleString('ru-RU',{maximumFractionDigits:1});
const date=(day:string)=>day.slice(8,10)+'.'+day.slice(5,7);

export default function ParserErrorHistory({history}:{history:ErrorHistory}){
 const id=useId(),{points,average}=history,today=points.at(-1);
 const width=920,left=40,top=16,bottom=184,plotWidth=width-left-12,step=plotWidth/Math.max(1,points.length);
 const tick=Math.max(1,Math.ceil(Math.max(1,...points.flatMap(p=>[p.p1??0,p.p2??0]))/4)),max=tick*4,y=(n:number)=>bottom-n/max*(bottom-top);
 return <article className="settings-card parser-error-history">
  <div><h2>Ошибки за 30 дней</h2><p className="resume-hint">Дневной пик активных ошибок по Москве. P1 — критичные сигналы, P2 — проблемные задания.</p></div>
  <div className="parser-history-metrics">{priorities.map(p=><div key={p.key} className={'parser-history-metric '+p.key}>
   <h3>{p.title}</h3><strong>{value(average[p.key])}<span> в день</span></strong><p>Среднее за 30 дней</p><p className="resume-hint">Сегодня, пик: {value(today?.[p.key]??null)}</p>
  </div>)}</div>
  <p className="resume-hint">Данных: {history.observedDays} из {history.days} дней. Среднее — по дням с наблюдениями; текущий день неполный. Пропуски не считаются нулём.</p>
  <div className="parser-history-legend">{priorities.map(p=><span key={p.key}><i style={{background:p.color}}/>{p.title}</span>)}<span><i className="mean"/>Среднее</span><span><i className="missing"/>Нет данных</span></div>
  <div className="parser-history-scroll"><svg viewBox={'0 0 '+width+' 224'} role="img" aria-labelledby={id+'-title '+id+'-desc'} className="parser-history-chart">
   <title id={id+'-title'}>Дневные пики ошибок P1 и P2 за последние 30 дней</title>
   <desc id={id+'-desc'}>Столбцы — дневной максимум. Пунктир — среднее по наблюдённым дням. Пропуски отмечены серым. Точные значения доступны в таблице под графиком.</desc>
   {[0,1,2,3,4].map(i=><g key={i}><line x1={left} x2={width-12} y1={y(i*tick)} y2={y(i*tick)} className="parser-history-grid"/><text x={left-8} y={y(i*tick)+4} textAnchor="end">{i*tick}</text></g>)}
   {points.map((point,i)=>{const x=left+i*step;return <g key={point.day} tabIndex={0} aria-label={date(point.day)+': P1 '+value(point.p1)+', P2 '+value(point.p2)}>
    <title>{date(point.day)+' · '+(point.p1===null?'нет наблюдений':'P1: '+point.p1+' · P2: '+point.p2)}</title>
    {point.p1===null?<rect x={x+step*.25} y={bottom-5} width={step*.5} height={5} className="parser-history-gap"/>:priorities.map((p,j)=>{const n=point[p.key]??0;return <rect key={p.key} x={x+step*(.12+j*.4)} y={n?y(n):bottom-1} width={step*.32} height={n?bottom-y(n):1} fill={p.color}/>;})}
    {(i%5===0||i===points.length-1)&&<text x={x+step*.5} y={bottom+23} textAnchor="middle">{date(point.day)}</text>}
   </g>;})}
   {priorities.map(p=>average[p.key]!==null&&<line key={p.key} x1={left} x2={width-12} y1={y(average[p.key]!)} y2={y(average[p.key]!)} stroke={p.color} strokeWidth="2" strokeDasharray="6 4"/>) }
  </svg></div>
  <p className="resume-hint">Плановые проверки и улучшения в график не входят. Повторные замеры сохраняют максимум, а не суммируют ошибки. Изменение правил сбора влияет на сравнимость дней.</p>
  <details><summary>Значения по дням</summary><div className="docs-table-wrap"><table className="parser-history-table"><thead><tr><th>Дата</th><th>P1</th><th>P2</th></tr></thead><tbody>{points.map(p=><tr key={p.day}><td>{date(p.day)}.{p.day.slice(0,4)}</td><td>{p.p1??'Нет данных'}</td><td>{p.p2??'Нет данных'}</td></tr>)}</tbody></table></div></details>
 </article>;
}
