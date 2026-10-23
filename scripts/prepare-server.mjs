import fs from 'node:fs';
import {buildDocumentation} from './documentation.mjs';
import {buildBackupSchema} from './backup-schema.mjs';
const root=process.cwd();fs.mkdirSync('server/generated',{recursive:true});
fs.writeFileSync('server/generated/documentation.json',JSON.stringify(buildDocumentation(root)));
fs.writeFileSync('server/generated/backup-schema.json',JSON.stringify(buildBackupSchema(root)));
