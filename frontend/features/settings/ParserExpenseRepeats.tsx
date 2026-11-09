import {useState} from 'react';
import {useApi} from '../../api/client';
import {State} from '../../components/Common';

interface RepeatApify {
 actorOperations:number;localReceipts:number;repeatedReceiptGroups:number;extraLocalReceipts:number;
 conflictingActorGroups:number;suspectedLaunchGroups:number;actorsInSuspectedGroups:number;
 missingActorIdentity:number;unknownStartReserveUsd:number;
}
interface RepeatStage {
 type:string;repeatGroups:number;extraAttempts:number;knownCostAttempts:number;
 knownLocalCostUsd:number;unknownCostAttempts:number;unknownReserveUsd:number;unpricedAttempts:number;
}
interface RepeatAI {observedAttempts:number;repeatGroups:number;extraAttempts:number;stages:RepeatStage[];}
export interface ExpenseRepeatsReport {
 checkedAt:number;timezone:string;periodDays:7|30;from:string;to:string;
 apify:RepeatApify;ai:RepeatAI;providerRepeatVerified:boolean;windowLimited:boolean;
}
const count=(v:number)=>v.toLocaleString('ru-RU');
const money=(v:number)=>v>0&&v<0.00001?'менее $0,00001':'$'+v.toLocaleString('ru-RU',{maximumFractionDigits:5});
const stageNames:Record<string,string>={
 classify_title:'Классификация',extract_vacancy:'Извлечение структуры',
 assign_roles:'Назначение ролей',fetch_full_text:'Полный текст',
 score_relevance:'Оценка релевантности',other:'Другие этапы'
};
const object=(v:unknown):v is Record<string,unknown>=>v!==null&&typeof v==='object'&&!Array.isArray(v);
const nonnegative=(v:unknown):v is number=>typeof v==='number'&&Number.isFinite(v)&&v>=0;
const integer=(v:unknown):v is number=>nonnegative(v)&&Number.isSafeInteger(v);
const date=(v:unknown):v is string=>typeof v==='string'&&/^\d{4}-\d{2}-\d{2}$/.test(v)&&
 Number.isFinite(Date.parse(v+'T00:00:00Z'))&&new Date(v+'T00:00:00Z').toISOString().slice(0,10)===v;
const validCounts=(v:unknown,keys:string[]):v is Record<string,unknown>=>object(v)&&keys.every(k=>integer(v[k]));
export function isExpenseRepeatsReport(v:unknown):v is ExpenseRepeatsReport {
 if(!object(v)||!(v.periodDays===7||v.periodDays===30)||v.timezone!=='Europe/Moscow'||
    !integer(v.checkedAt)||!date(v.from)||!date(v.to)||v.from>v.to||
    v.providerRepeatVerified!==false||v.windowLimited!==true||
    !validCounts(v.apify,['actorOperations','localReceipts','repeatedReceiptGroups',
      'extraLocalReceipts','conflictingActorGroups','suspectedLaunchGroups','actorsInSuspectedGroups',
      'missingActorIdentity'])||
    !nonnegative(v.apify.unknownStartReserveUsd)||
    !validCounts(v.ai,['observedAttempts','repeatGroups','extraAttempts'])||
    !Array.isArray(v.ai.stages))return false;
 return v.ai.stages.every((s:unknown)=>object(s)&&
   typeof s.type==='string'&&s.type in stageNames&&
   ['repeatGroups','extraAttempts','knownCostAttempts','unknownCostAttempts','unpricedAttempts'].every(k=>integer(s[k]))&&
   nonnegative(s.knownLocalCostUsd)&&nonnegative(s.unknownReserveUsd));
}

export function ExpenseRepeatsView({report:r}:{report:ExpenseRepeatsReport}){
 const {apify:a,ai}=r;
 return <>
  <p className="resume-hint">{r.from}–{r.to}, календарные дни по Москве. Диагностика ограничена выбранным периодом; операции вне него не сопоставляются.</p>
  <p className="resume-hint" role="note"><strong>Это признаки повторов, не подтверждение повторной оплаты.</strong> Совпадение локальных квитанций одного Apify Actor — повтор учёта одной операции, а не второй платный запуск. Новые Actor с одинаковым ключом могут требовать проверки провайдером. Для ИИ нет надёжных ID отдельных внешних запросов.</p>
  <div className="parser-funnel-totals">
   <div><span>Apify · повторные квитанции</span><strong>{count(a.repeatedReceiptGroups)}</strong><span>Групп одного Actor · лишних локальных записей: {count(a.extraLocalReceipts)}</span></div>
   <div><span>Apify · возможные повторные старты</span><strong>{count(a.suspectedLaunchGroups)}</strong><span>Групп одного ключа с разными Actor: {count(a.actorsInSuspectedGroups)} запусков</span></div>
   <div><span>ИИ · повторные попытки заданий</span><strong>{count(ai.repeatGroups)}</strong><span>Дополнительных попыток: {count(ai.extraAttempts)}</span></div>
  </div>
  <p className="resume-hint">Apify: {count(a.actorOperations)} различных Actor за период из {count(a.localReceipts)} локальных записей. Конфликтующих цен одного Actor: {count(a.conflictingActorGroups)} групп (требуют сверки). Без доказанной идентичности Actor: {count(a.missingActorIdentity)} записей; связанные с ними нерешённые резервы: {money(a.unknownStartReserveUsd)}, <strong>не подтверждённые списания</strong>.</p>
  <h3>Повторные ИИ-попытки по этапам</h3>
  <p className="resume-hint">Совпадают локальные ID задания, тип этапа и провайдер. Повтор может быть правомерным retry, изменением входа или модели. Известные расходы — из локальных квитанций ИИ-попыток внутри повторных групп, а не дополнительная сумма к отчёту себестоимости.</p>
  <div className="docs-table-wrap"><table className="parser-history-table"><thead><tr>
   <th>Этап</th><th>Групп повторов</th><th>Доп. попыток</th><th>С локальной ценой</th><th>Известные USD</th><th>Неизвестный исход</th><th>Без цены</th>
  </tr></thead><tbody>{ai.stages.map(s=><tr key={s.type}><td>{stageNames[s.type]}</td>
   <td>{count(s.repeatGroups)}</td><td>{count(s.extraAttempts)}</td><td>{count(s.knownCostAttempts)}</td>
   <td>{money(s.knownLocalCostUsd)}</td><td>{count(s.unknownCostAttempts)} · резерв {money(s.unknownReserveUsd)}</td>
   <td>{count(s.unpricedAttempts)}</td></tr>)}</tbody></table></div>
  {!ai.stages.length&&<p role="status">В выбранном периоде повторных ИИ-попыток одного задания не обнаружено. Это не доказывает отсутствие повторных вызовов провайдера.</p>}
  <p className="resume-hint">Анализ ИИ включает только сохранённые попытки OpenAI/OpenRouter в планировщике ({count(ai.observedAttempts)} за период). Он не охватывает все внешние обращения и не подтверждает, что повторное извлечение Firecrawl было оплачено. Сверка реальных списаний остаётся отдельной задачей Codex.</p>
 </>;
}

export default function ParserExpenseRepeats(){
 const [days,setDays]=useState<7|30>(30);
 const q=useApi<ExpenseRepeatsReport>('/api/scheduler/expense-repeats?days='+days,{refreshIntervalMs:60000});
 const valid=isExpenseRepeatsReport(q.data)?q.data:undefined;
 const report=valid?.periodDays===days&&!q.error?valid:undefined;
 const invalid=q.data!==undefined&&!valid&&!q.loading&&!q.error;
 const error=q.error??(invalid?new Error('Некорректный ответ диагностики повторов.'):undefined);
 return <article className="settings-card parser-expense-repeats"><div className="parser-line">
  <div><h2>Повторные операции парсера</h2><p className="resume-hint">GO-434 · только чтение локальных журналов</p></div>
  <button type="button" className="settings-button secondary" onClick={q.reload}>Обновить</button>
  </div>
  <div className="parser-funnel-filters"><label><span>Период</span><select className="settings-select" aria-label="Период диагностики повторов" value={days} onChange={e=>setDays(Number(e.target.value) as 7|30)}><option value={7}>7 дней</option><option value={30}>30 дней</option></select></label></div>
  <State loading={!report&&q.loading} error={error}>{report?<ExpenseRepeatsView report={report}/>:!q.loading&&!error&&<p>Диагностика пока недоступна.</p>}</State>
 </article>;
}
