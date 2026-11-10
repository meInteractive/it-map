import type { DebugJSONInput } from './debug-json.mjs';
export type AllDiagnosticsSectionName =
  | 'schedulerOverview'
  | 'queue'
  | 'runs'
  | 'attempts'
  | 'aiSpending'
  | 'apify'
  | 'firecrawl'
  | 'issues'
  | 'funnel';
export declare const ALL_DIAGNOSTICS_SECTIONS: Readonly<Record<AllDiagnosticsSectionName, string>>;
export declare function composeAllDiagnostics(
  input: Partial<Record<AllDiagnosticsSectionName, Omit<DebugJSONInput, 'section'>>>,
  capturedAt?: string,
  appVersion?: string | null,
): string;
