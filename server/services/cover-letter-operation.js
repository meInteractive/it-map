import {claimCoverLetterGeneration,readCoverLetterGeneration,coverLetterInputHash,coverLetterGenerationSnapshot,renewCoverLetterGeneration,lostCoverLetterGeneration} from '../repositories/cover-letter-generation.js';
import {readCoverLetterInput,existingCoverLetter,changedCoverLetterInput} from './cover-letter-input.js';
import {restoredDuringOperation} from '../repositories/runtime-generation.js';

export function coverLetterCurrent(input,current) {
 if(!current)return null;
 if(current.generation!==input.generation)throw restoredDuringOperation();
 if(input.regenerate){
  if(current.snapshot!==input.snapshot)throw changedCoverLetterInput();
  if(current.personalSnapshot!==input.personalSnapshot){
   const saved=existingCoverLetter(current);
   if(saved)return saved;
   throw changedCoverLetterInput();
  }
  return undefined;
 }
 const saved=existingCoverLetter(current);
 if(saved)return saved;
 if(current.snapshot!==input.snapshot)throw changedCoverLetterInput();
 return undefined;
}
const pause=()=>new Promise(resolve=>setTimeout(resolve,1000));
// Shared D1 is the authority. Poll saved text; only expired ownership can recover
// automatically. A failed owner releases its slot; waiters report failure, not N retries.
export async function acquireCoverLetterGeneration(db,userName,vacancyId,input,onProgress) {
 const lease=await claimCoverLetterGeneration(db,userName,vacancyId,input);
 if(lease)return {lease};
 const hash=await coverLetterInputHash(coverLetterGenerationSnapshot(input)),started=Date.now();
 for(;;){
  const current=coverLetterCurrent(input,await readCoverLetterInput(db,userName,vacancyId));
  if(current!==undefined)return {letter:current};
  const owner=await readCoverLetterGeneration(db,userName,vacancyId);
  if(!owner){
   // Saved text may have committed between our preceding read and release.
   const latest=coverLetterCurrent(input,await readCoverLetterInput(db,userName,vacancyId));
   if(latest!==undefined)return {letter:latest};
   throw Object.assign(new Error('Параллельная генерация завершилась без письма. Повторите запрос.'),{status:503,code:'generation_failed'});
  }
  if(owner.generation!==input.generation)throw restoredDuringOperation();
  if(owner.inputHash!==hash)throw lostCoverLetterGeneration();
  if(!owner.active){
   const recovered=await claimCoverLetterGeneration(db,userName,vacancyId,input);
   if(recovered)return {lease:recovered};
  }
  if(Date.now()-started>=15*60*1000)throw Object.assign(new Error('Генерация ещё выполняется. Повторите запрос, чтобы получить результат.'),{status:503,code:'generation_busy'});
  onProgress?.({stage:'waiting'});
  await pause();
 }
}
export function coverLetterHeartbeat(db,lease,input) {
 const controller=new AbortController();let timer,pending,stopped=false;
 async function check(){
  controller.signal.throwIfAborted();
  if(!pending)pending=(async()=>{
   try{if(!await renewCoverLetterGeneration(db,lease,input))throw lostCoverLetterGeneration();}
   catch(error){controller.abort(error);throw error;}
   finally{pending=undefined;}
  })();
  return pending;
 }
 async function tick(){
  try{await check();if(!stopped)timer=setTimeout(tick,20000);}catch{}
 }
 timer=setTimeout(tick,20000);
 return {signal:controller.signal,check,async stop(){stopped=true;clearTimeout(timer);await pending?.catch(()=>{});}};
}
