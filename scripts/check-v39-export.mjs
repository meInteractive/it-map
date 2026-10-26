// Validate a synthetic CI export from stdin; never create an archive on disk.
import {readFileSync} from 'node:fs';
import {validateFullBackup} from '../server/backup/index.js';
globalThis.fetch=()=>{throw new Error('Export oracle forbids network access');};
try {
 const body=readFileSync(0,'utf8');
 if(Buffer.byteLength(body)>25*1024*1024)throw new Error();
 const payload=JSON.parse(body),tables=await validateFullBackup(payload);
 if(payload.version!==39||tables.length!==52)throw new Error();
 console.log('PASS: unchanged legacy validator accepts synthetic Go v39 export');
} catch {console.error('FAIL: synthetic Go export rejected by legacy v39 validator');process.exitCode=1;}
