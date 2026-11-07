export interface QueueDebugResponse {
  items: Record<string,unknown>[];
  total:number;
  coverage:'server_latest';
  limit:number;
}
export declare function schedulerOverviewSnapshot(d:Record<string,any>):Record<string,unknown>;
export declare function schedulerQueueSnapshot(d:QueueDebugResponse, filters:{status:string;type:string;searchApplied:boolean}):Record<string,unknown>;
export declare function schedulerRunsSnapshot(d:{runs:Record<string,any>[]}):Record<string,unknown>;
export declare function schedulerAttemptsSnapshot(d:{attempts:Record<string,any>[]}):Record<string,unknown>;
