export type DebugJSONSection =
  | 'scheduler_overview'
  | 'scheduler_queue'
  | 'scheduler_runs'
  | 'scheduler_attempts'
  | 'ai_spending'
  | 'apify_usage'
  | 'firecrawl_usage'
  | 'parser_issues'
  | 'parser_funnel';

export interface DebugJSONInput {
  section: DebugJSONSection;
  capturedAt?: string | number;
  dataAsOf?: string | number | null;
  lastSuccessfulAt?: string | number | null;
  appVersion?: string | null;
  available?: boolean;
  errorCode?: string;
  filters?: Record<string,unknown>;
  summary?: Record<string,unknown>;
  items?: Record<string,unknown>[];
  total?: number;
  coverage?: 'complete' | 'server_latest';
  daily?: Record<string,unknown>[];
  breakdown?: Record<string,unknown>[];
}
export declare const DEBUG_JSON_SCHEMA_VERSION: 1;
export declare const DEBUG_JSON_EVENT_LIMIT: 10;
export declare const DEBUG_JSON_MAX_BYTES: number;
export declare const DEBUG_JSON_SECTIONS: readonly DebugJSONSection[];
export declare function buildDebugJSON(input: DebugJSONInput): Record<string,unknown>;
export declare function serializeDebugJSON(input: DebugJSONInput): string;
