import type {DebugJSONInput} from './debug-json.mjs';

interface ApifyBudgetSnapshot {
  providerUsedUsd?:number; ownConfirmedUsd?:number; readReserveUsd?:number;
  unknownReserveUsd?:number; unknownRuns?:number; limitUsd?:number;
  remainingUsd?:number; periodStart?:string; periodEnd?:string; checkedAt?:number;
}
interface ApifyUsageSnapshot {
  providerLimitMode?:boolean;
  profiles?:{free:ApifyBudgetSnapshot|null;starter:ApifyBudgetSnapshot|null};
  active?:Array<{id:string;profile:string;state:string;created_at:number}>;
}
interface ApifyOperationsHistory {
  checkedAt:number;coverage:'server_latest';total:number;
  items:Array<{
    operationId:string;profile:string;state:string;
    createdAt:number;updatedAt:number;chargedUsd:number|null;
    reservedUsd:number;errorCode:string|null;
  }>;
}
export declare function apifyUsageSnapshot(
  usage:ApifyUsageSnapshot,
  operations?:ApifyOperationsHistory|null,
):Omit<DebugJSONInput,'section'>;

interface FirecrawlBudgetSnapshot {
  providerPlanCredits:number;
  providerRemaining:number;
  providerUsedEstimate:number|null;
  localReported:number;
  localEstimated:number;
  reserved:number;
  effectiveUsed:number;
  remaining:number;
  periodStart:string;
  periodEnd:string;
}
interface FirecrawlUsageSnapshot {
  mode:'free'|'hobby';
  fresh:boolean;
  status:string;
  checkedAt:number|null;
  limits:{credits:number};
  budget:FirecrawlBudgetSnapshot|null;
}
export declare function firecrawlUsageSnapshot(view:FirecrawlUsageSnapshot,now?:number):Omit<DebugJSONInput,'section'>;
