import {SCHEDULER_APIFY_POLICY} from '../repositories/scheduler-schema.js';

// A slot conflict is short-lived; a budget denial needs a fresh account check.
// Neither consumes a retry attempt or creates a new provider operation identity.
export function schedulerWaitDelay(code,{targeted=false}={}){
 if(['slice','apify_cycle_limit','time_slice'].includes(code))return 1000;
 if(code==='apify_running')return targeted?1000:SCHEDULER_APIFY_POLICY.pollDelayMs;
 if(code==='apify_busy')return SCHEDULER_APIFY_POLICY.pollDelayMs;
 if(['apify_budget','apify_uncertain','apify_changed'].includes(code))return 300000;
 return 3600000;
}
