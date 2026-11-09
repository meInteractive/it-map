import {useRef,useState} from 'react';
import {useApi} from '../../api/client';
import {State} from '../../components/Common';

interface Spending {
 confirmedUsd:number;
 confirmedOperations:number;
 unknownReservedUsd:number;
 unknownOperations:number;
 unpricedOperations:number;
 conflictingOperations:number;
 unavailableForSource:boolean;
}
interface CostDay {
 day:string;newVacancies:number;aiUsd:number;apifyUsd:number;observedCredits:number;
}
export interface UnitCostReport {
 checkedAt:number;timezone:string;from:string;to:string;periodDays:7|30;
 sourceId:string;sourceOptions:{id:string;name:string}[];
 newUniqueVacancies:number;undatedCreates:number;
 apify:Spending;ai:Spending;aiLineage:AILineage;
 firecrawlCredits:number;unknownCreditRows:number;
 totalConfirmedUsd:number;partialPeriodRatioUsd:number|null;
 calendarProxy:boolean;incompleteCosts:boolean;daily:CostDay[];
}
// The API cache accepts untyped JSON. Never turn a partial/invalid financial
// report into a displayed zero, a misleading USD total or an uncaught render error.
type ReportObject=Record<string,unknown>;
const object=(v:unknown):v is ReportObject=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const amount=(v:unknown):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=0;
const integer=(v:unknown):v is number=>amount(v)&&Number.isSafeInteger(v);
const sameAmount=(a:number,b:number)=>Math.abs(a-b)<=1e-8*Math.max(1,Math.abs(a),Math.abs(b));
const validDay=(v:unknown):v is string=>{
 if(typeof v!=='string'||!/^\d{4}-\d{2}-\d{2}$/.test(v))return false;
 const time=Date.parse(v+'T00:00:00Z');
 return Number.isFinite(time)&&new Date(time).toISOString().slice(0,10)===v;
};
const validSpend=(v:unknown):v is Spending=>object(v)&&
 ['confirmedUsd','unknownReservedUsd'].every(k=>amount(v[k]))&&
 ['confirmedOperations','unknownOperations','unpricedOperations','conflictingOperations'].every(k=>integer(v[k]))&&
 typeof v.unavailableForSource==='boolean';
const validLineage=(v:unknown):v is AILineage=>object(v)&&
 ['attempts','withJob','withCandidate','withMainVacancy','withMatchingCreate','missingJob',
 'missingCandidate','missingMainVacancy','sourceMismatch'].every(k=>integer(v[k]))&&
 ['knownUsdWithMainLink','knownUsdWithoutMainLink'].every(k=>amount(v[k]))&&
 typeof v.verifiedProviderAttribution==='boolean';

export function isUnitCostReport(v:unknown):v is UnitCostReport {
 if(!object(v)||!(v.periodDays===7||v.periodDays===30)||typeof v.sourceId!=='string'||!v.sourceId||
    v.timezone!=='Europe/Moscow'||!integer(v.checkedAt)||!validDay(v.from)||!validDay(v.to)||
    !integer(v.newUniqueVacancies)||!integer(v.undatedCreates)||
    !validSpend(v.apify)||!validSpend(v.ai)||!validLineage(v.aiLineage)||
    !amount(v.firecrawlCredits)||!integer(v.unknownCreditRows)||
    !amount(v.totalConfirmedUsd)||(v.partialPeriodRatioUsd!==null&&!amount(v.partialPeriodRatioUsd))||
    v.calendarProxy!==true||v.incompleteCosts!==true||
    !Array.isArray(v.sourceOptions)||!v.sourceOptions.every(x=>object(x)&&typeof x.id==='string'&&
      x.id.length>0&&typeof x.name==='string')||
    !Array.isArray(v.daily)||v.daily.length!==v.periodDays)return false;
 let totalNew=0,totalAI=0,totalApify=0;
 let previous=0;
 for(const day of v.daily){
  if(!object(day)||!validDay(day.day)||!integer(day.newVacancies)||!amount(day.aiUsd)||
     !amount(day.apifyUsd)||!amount(day.observedCredits))return false;
  const epoch=Date.parse(day.day+'T00:00:00Z');
  if(previous&&epoch-previous!==86400000)return false;
  previous=epoch;
  totalNew+=day.newVacancies;
  totalAI+=day.aiUsd;
  totalApify+=day.apifyUsd;
 }
 if(v.daily[0].day!==v.from||v.daily[v.daily.length-1].day!==v.to||
    !Number.isSafeInteger(totalNew)||totalNew!==v.newUniqueVacancies||
    !sameAmount(v.totalConfirmedUsd,v.ai.confirmedUsd+v.apify.confirmedUsd)||
    !sameAmount(totalAI,v.ai.confirmedUsd)||!sameAmount(totalApify,v.apify.confirmedUsd)||
    (v.newUniqueVacancies===0&&v.partialPeriodRatioUsd!==null))return false;
 return v.partialPeriodRatioUsd===null||
   sameAmount(v.partialPeriodRatioUsd,v.totalConfirmedUsd/v.newUniqueVacancies);
}
const money=(n:number)=>n>0&&n<0.00001?'менее $0,00001':'$'+n.toLocaleString('ru-RU',{maximumFractionDigits:5});
const count=(n:number)=>n.toLocaleString('ru-RU');
const date=(v:string)=>v.slice(8,10)+'.'+v.slice(5,7);
const axisAmount=(n:number)=>n===0?'0':n.toLocaleString('ru-RU',{maximumSignificantDigits:3});

export function UnitCostView({report}:{report:UnitCostReport}){
 const {apify,ai,daily}=report;
 const barMax=Math.max(0,...daily.map(d=>d.aiUsd+d.apifyUsd)),scale=barMax>0?barMax:1;
 const width=820,left=42,right=16,bottom=184,chartHeight=148;
 const step=(width-left-right)/daily.length,barWidth=Math.min(23,step*.65);
 const bars=daily.map((d,i)=>{
  const x=left+i*step+(step-barWidth)/2;
  const aiH=d.aiUsd/scale*chartHeight;
  const apifyH=d.apifyUsd/scale*chartHeight;
  return <g key={d.day}><title>{date(d.day)}: ИИ {money(d.aiUsd)}, Apify {money(d.apifyUsd)}; новых вакансий {d.newVacancies}</title>
   <rect x={x} y={bottom-aiH} width={barWidth} height={aiH} fill="#627fbd"/>
   <rect x={x} y={bottom-aiH-apifyH} width={barWidth} height={apifyH} fill="#b58a49"/>
   {(i%(report.periodDays===7?1:5)===0||i===daily.length-1)&&<text x={x+barWidth/2} y={bottom+19} textAnchor="middle" fontSize="12">{date(d.day)}</text>}
  </g>;
 });
 return <>
  <div className="parser-funnel-totals">
   <div><span>Новые уникальные карточки</span><strong>{count(report.newUniqueVacancies)}</strong><span>Только первый успешный create</span></div>
   <div><span>Локально подтверждённые расходы</span><strong>{money(report.totalConfirmedUsd)}</strong><span>Не включая резервы и Firecrawl</span></div>
   <div><span>Частичный ориентир на вакансию</span><strong>{report.partialPeriodRatioUsd===null?'Нет значения':money(report.partialPeriodRatioUsd)}</strong><span>Расходы периода / новые карточки периода</span></div>
  </div>
  <p className="resume-hint" role="note">Это <strong>календарный ориентир</strong>, а не полная себестоимость когорты сканирования. Расходы и первые публикации могли относиться к разным запускам, в том числе завершённым позже. Неизвестная стоимость Firecrawl и неполная связь Apify с источниками не включены в USD.</p>
  {report.newUniqueVacancies===0&&<p role="status">За период новых карточек нет. Деление на ноль не выполняется; уже понесённые расходы показаны отдельно.</p>}
  <div className="parser-funnel-totals">
   <div><span>Apify · известные Actor</span><strong>{apify.unavailableForSource?'Не распределено':money(apify.confirmedUsd)}</strong><span>{apify.unavailableForSource?'У Actor нет достоверной связи с выбранным источником':'Одна операция учитывается один раз'}</span></div>
   <div><span>ИИ · известные попытки</span><strong>{money(ai.confirmedUsd)}</strong><span>OpenRouter и OpenAI</span></div>
   <div><span>Firecrawl · наблюдения</span><strong>{count(report.firecrawlCredits)} кредитов</strong><span>Это не стоимость в USD; без данных: {count(report.unknownCreditRows)} снимков</span></div>
  </div>
  <p className="resume-hint">Неопределённые резервы: ИИ {money(ai.unknownReservedUsd)} ({count(ai.unknownOperations)}), Apify {apify.unavailableForSource?'без распределения':money(apify.unknownReservedUsd)+' ('+count(apify.unknownOperations)+')'}. Операций без цены: ИИ {count(ai.unpricedOperations)}, Apify {apify.unavailableForSource?'—':count(apify.unpricedOperations)}. Конфликтующих записей: {count(ai.conflictingOperations+(apify.unavailableForSource?0:apify.conflictingOperations))}.</p>
  {report.undatedCreates>0&&<p className="resume-hint">Созданий с недостоверной датой: {report.undatedCreates}. Они не добавлены в периодный знаменатель.</p>}
  <AILineageView lineage={report.aiLineage}/><h3>Динамика локально подтверждённых расходов</h3>
  <p className="resume-hint">По дате операции, московское время; <span style={{color:'#627fbd'}}>■</span> ИИ, <span style={{color:'#b58a49'}}>■</span> Apify. Количество новых вакансий по дням — в таблице под графиком. Неподтверждённые списания на график не попадают.</p>
  <div className="parser-history-scroll"><svg className="parser-history-chart" viewBox={'0 0 '+width+' 224'} role="img" aria-label="Подтверждённые локальные расходы Apify и ИИ по дням в USD">
   {[0,1,2,3,4].map(i=><g key={i}><line className="parser-history-grid" x1={left} x2={width-right} y1={bottom-chartHeight*i/4} y2={bottom-chartHeight*i/4}/><text x={left-6} y={bottom-chartHeight*i/4+4} textAnchor="end" fontSize="11">{axisAmount(barMax*i/4)}</text></g>)}
   {bars}
  </svg></div>
  <details><summary>Подробные значения по дням</summary><div className="docs-table-wrap"><table className="parser-history-table"><thead><tr><th>Дата</th><th>Новых карточек</th><th>Apify USD</th><th>ИИ USD</th><th>Кредиты Firecrawl (наблюдения)</th></tr></thead><tbody>{daily.map(d=><tr key={d.day}><td>{d.day}</td><td>{d.newVacancies}</td><td>{money(d.apifyUsd)}</td><td>{money(d.aiUsd)}</td><td>{d.observedCredits}</td></tr>)}</tbody></table></div></details>
 </>;
}

export default function ParserUnitCost(){
 const [days,setDays]=useState<7|30>(30),[sourceId,setSourceId]=useState('all');
 const options=useRef<UnitCostReport|null>(null);
 const q=useApi<UnitCostReport>('/api/scheduler/unit-cost?days='+days+'&sourceId='+encodeURIComponent(sourceId),{refreshIntervalMs:60000});
 const raw=isUnitCostReport(q.data)?q.data:undefined;
 if(raw)options.current=raw;
 const invalidResponse=q.data!==undefined&&!raw&&!q.loading&&!q.error;
 const report=raw?.periodDays===days&&raw.sourceId===sourceId&&!q.error?raw:undefined;
 const displayError=q.error??(invalidResponse?new Error('Некорректный ответ статистики. Повторите загрузку.'):undefined);
 return <article className="settings-card parser-unit-cost"><div className="parser-line"><div><h2>Себестоимость новой вакансии</h2><p className="resume-hint">Частичный календарный расчёт · GO-433</p></div><button className="settings-button secondary" type="button" onClick={q.reload}>Обновить</button></div>
  <div className="parser-funnel-filters"><label><span>Период</span><select className="settings-select" aria-label="Период расчёта стоимости" value={days} onChange={e=>setDays(Number(e.target.value) as 7|30)}><option value={7}>7 дней</option><option value={30}>30 дней</option></select></label>
  <label><span>Источник</span><select className="settings-select" aria-label="Источник расчёта стоимости" value={sourceId} onChange={e=>setSourceId(e.target.value)}><option value="all">Все источники</option>{options.current?.sourceOptions.map(o=><option key={o.id} value={o.id}>{o.name}</option>)}</select></label></div>
  <State loading={!report&&q.loading} error={displayError}>{report?<><p className="resume-hint">{date(report.from)}–{date(report.to)}.{report.to.slice(0,4)}, по Москве.</p><UnitCostView report={report}/></>:!q.loading&&<p>Статистика стоимости пока недоступна.</p>}</State>
 </article>;
}

interface AILineage {
 attempts:number;withJob:number;withCandidate:number;withMainVacancy:number;
 withMatchingCreate:number;missingJob:number;missingCandidate:number;missingMainVacancy:number;
 sourceMismatch:number;knownUsdWithMainLink:number;knownUsdWithoutMainLink:number;
 verifiedProviderAttribution:boolean;
}

// Only evidence coverage. An AI attempt with a vacancy link is NOT a cost
// assigned to that vacancy: the provider request may serve several objects.
export function AILineageView({lineage:l}:{lineage:AILineage}){
 const fraction=l.attempts?Math.round(l.withMainVacancy/l.attempts*100):null;
 return <section className="parser-unit-cost-lineage" aria-label="Прослеживаемость расходов ИИ">
  <h3>Прослеживаемость расходов ИИ</h3>
  <p className="resume-hint">Связи в локальной БД, а не доказанная финансовая атрибуция. Один ИИ-запрос мог обслужить несколько задач, а существующая карточка могла быть создана другим запуском.</p>
  <div className="parser-funnel-totals">
   <div><span>ИИ-попыток за период</span><strong>{count(l.attempts)}</strong><span>С заданием: {count(l.withJob)}</span></div>
   <div><span>С кандидатом</span><strong>{count(l.withCandidate)}</strong><span>Из сохранённых попыток</span></div>
   <div><span>С действующей основной карточкой</span><strong>{count(l.withMainVacancy)}</strong><span>{fraction===null?'Нет операций':fraction+'% попыток'} · с matching create: {count(l.withMatchingCreate)}</span></div>
  </div>
  <p className="resume-hint">Известные локальные расходы ИИ со связью до основной карточки: {money(l.knownUsdWithMainLink)}; без такой связи: {money(l.knownUsdWithoutMainLink)}. Эти суммы <strong>не распределены</strong> между вакансиями.</p>
  <details><summary>Разрывы и расхождения связей</summary>
   <p>Задание отсутствует: {count(l.missingJob)}. Указанный кандидат отсутствует: {count(l.missingCandidate)}. Указанная основная карточка отсутствует: {count(l.missingMainVacancy)}. Источник попытки и задания расходится: {count(l.sourceMismatch)}.</p>
   <p className="resume-hint">Для Apify нет универсального ключа Actor → задание/кандидат; для Firecrawl нет подтверждённой исторической стоимости USD. Этот отчёт показывает только покрытие записей ИИ планировщика, а не завершённость всей цепочки расходов.</p>
  </details>
 </section>;
}
