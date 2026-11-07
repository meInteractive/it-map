import CopyDebugJSON from './CopyDebugJSON';
import {useApi} from '../../api/client';
import {aiSpendingSnapshot,type AISpendingDebugReport} from '../../../shared/ai-spending-debug.mjs';

interface Props {dayBudgetUsd:number;monthBudgetUsd:number;}

// Бюджеты отображаются прежним компонентом. Этот блок только копирует
// реальный локальный ledger, не инициируя платных вызовов.
export default function AISpendingDebug({dayBudgetUsd,monthBudgetUsd}:Props) {
 const q=useApi<AISpendingDebugReport>('/api/scheduler/ai-spending/debug');
 const session=useApi<{isAdmin:boolean}>('/api/session');
 const isAuthorized=()=>Boolean(session.data?.isAdmin&&!session.error);
 return <>
  <CopyDebugJSON section="ai_spending" isAuthorized={isAuthorized}
   disabled={!q.data||!!q.error||!isAuthorized()}
   getSnapshot={()=>aiSpendingSnapshot(q.data!,{dayBudgetUsd,monthBudgetUsd})}/>
  {q.error&&<span role="status" className="resume-hint">Детальные расходы ИИ сейчас недоступны.</span>}
 </>;
}
