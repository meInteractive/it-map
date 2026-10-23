import {spawnSync} from 'node:child_process';
const result=spawnSync(process.execPath,['scripts/test-parser-luna-pipeline.mjs'],{env:{...process.env,IT_MAP_AGENCY_FIXTURE:'1'},stdio:'inherit'});process.exitCode=result.status??1;
