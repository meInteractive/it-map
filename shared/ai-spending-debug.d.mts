import type {DebugJSONInput} from './debug-json.mjs';
interface AICostSummary {
  confirmedUsd:number;
  unknownReservedUsd:number;
  upperEstimateUsd:number;
  unknownCostOperations:number;
  unpricedOperations:number;
}
export interface AISpendingDebugReport {
  checkedAt:number;
  dayStart:number;
  monthStart:number;
  day:AICostSummary;
  month:AICostSummary;
  providers:Record<'openai'|'openrouter',AICostSummary>;
  models:Array<Record<string,unknown>>;
  latest:Array<Record<string,unknown>>;
  total:number;
  duplicateAICallsAvailable:boolean;
}
export declare function aiSpendingSnapshot(
  report:AISpendingDebugReport,
  budgets:{dayBudgetUsd:number;monthBudgetUsd:number},
):Omit<DebugJSONInput,'section'>;
