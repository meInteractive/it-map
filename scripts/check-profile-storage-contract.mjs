// Execute unchanged legacy settings SQL on an ephemeral SQLite :memory: database.
// No files, external database, credentials, provider or network calls exist.
import {readFile,writeFile} from 'node:fs/promises';
import {DatabaseSync} from 'node:sqlite';
import {createHash} from 'node:crypto';
globalThis.fetch=()=>{throw new Error('Profile storage oracle forbids network access');};
const {readSystemPrompts,saveSystemPrompt,PROMPT_KINDS}=await import('../server/repositories/prompts.js');
const {readPromptModels,savePromptModel}=await import('../server/repositories/prompt-models.js');
const schema=JSON.parse(await readFile(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8'));
const tables=['user_profiles','system_prompts','prompt_model_settings','vacancy_detail_settings','vacancy_role_settings'];
function database(){
 const memory=new DatabaseSync(':memory:');
 for(const name of tables){const object=schema.objects.find(o=>o.type==='table'&&o.name===name);if(!object?.sql)throw new Error('Missing source table DDL');memory.exec(object.sql);}
 return {close(){memory.close();},prepare(sql){const statement=memory.prepare(sql);let args=[];return {bind(...values){args=values;return this;},async run(){return statement.run(...args);},async first(){return statement.get(...args)??null;},async all(){return {results:statement.all(...args)};}};}};
}
const hash=s=>createHash('sha256').update(s,'utf8').digest('hex');
function prompt(value){return {kind:value.kind,textHash:hash(value.markdown),revision:value.revision,hasUpdatedAt:value.updatedAt!==null};}
function model(value){return {kind:value.kind,provider:value.provider,openaiModel:value.openaiModel,openrouterModel:value.openrouterModel,reasoningEffort:value.reasoningEffort,revision:value.revision,hasUpdatedAt:value.updatedAt!==null};}
const vectors=[];
for(const kind of PROMPT_KINDS)for(const first of ['prompt','model']){
 const db=database();
 try{
  const sequence=first==='prompt'?['prompt','model','prompt']:['model','prompt','model'];let promptRevision=0,modelRevision=0;
  const steps=[];
  for(const part of sequence){
   const input=part==='prompt'?{markdown:`CI source prompt ${promptRevision+1}`,revision:promptRevision}:{provider:'openrouter',openaiModel:'CI_model:1',openrouterModel:'best_free',reasoningEffort:modelRevision===0?'high':'low',revision:modelRevision};
   if(part==='prompt'){await saveSystemPrompt(db,kind,input);promptRevision++;}else{await savePromptModel(db,kind,input);modelRevision++;}
   const prompts=await readSystemPrompts(db),models=await readPromptModels(db);
   steps.push({part,input,prompt:prompt(prompts.prompts[kind]),model:model(models[kind])});
  }
  vectors.push({kind,first,steps});
 }finally{db.close();}
}
const output=JSON.stringify({oracle:'Unchanged legacy save/read functions with source v39 DDL in SQLite :memory:. Clock values projected to presence only; text stored as SHA256 for compact synthetic vectors.',vectors},null,2)+'\n';
const target=new URL('../internal/profiles/testdata/storage-contract.json',import.meta.url);
if(process.argv.length===3&&process.argv[2]==='--write')await writeFile(target,output);
else if(process.argv.length!==2)throw new Error('Use --write only for reviewed regeneration');
else if(await readFile(target,'utf8')!==output)throw new Error('Legacy profile storage transitions changed; review fixture');
console.log(`PASS: ${vectors.length*3} real legacy SQLite settings transitions; ephemeral memory only`);
