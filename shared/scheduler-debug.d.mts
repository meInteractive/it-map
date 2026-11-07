import type {DebugJSONInput} from './debug-json.mjs';
type Snapshot = Omit<DebugJSONInput,'section'>;
export interface QueueDebugResponse {
  items: Record<string,unknown>[];
  total:number;
  coverage:'server_latest';
  limit:number;
}
export declare function schedulerOverviewSnapshot(d:any):Snapshot;
export declare function schedulerQueueSnapshot(d:QueueDebugResponse, filters:{status:string;type:string;searchApplied:boolean}):Snapshot;
export declare function schedulerRunsSnapshot(d:any):Snapshot;
export declare function schedulerAttemptsSnapshot(d:any):Snapshot;
