import type {DebugJSONInput} from './debug-json.mjs';
import type {IssuesReport} from '../frontend/features/settings/ParserIssues';
import type {ParserFunnelReport} from './parser-funnel';

export declare function parserIssuesSnapshot(report:IssuesReport):Omit<DebugJSONInput,'section'>;
export declare function parserFunnelSnapshot(report:ParserFunnelReport):Omit<DebugJSONInput,'section'>;
