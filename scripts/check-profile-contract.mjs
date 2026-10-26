// Execute unchanged legacy input/read/route functions on synthetic objects only.
// DB.run/first/all below are in-memory spies; no SQL engine or provider exists.
import {readFile,writeFile} from 'node:fs/promises';
import {createHash} from 'node:crypto';
globalThis.fetch=()=>{throw new Error('Profile contract oracle forbids network access');};
const {readResumeInput,readUserResume:legacyReadResume}=await import('../server/repositories/resume.js');
const {readPromptInput,readSystemPrompts,saveSystemPrompt,PROMPT_KINDS}=await import('../server/repositories/prompts.js');
const {readPromptModelInput,readPromptModels,savePromptModel}=await import('../server/repositories/prompt-models.js');
const kinds=[...PROMPT_KINDS];
const hash=s=>createHash('sha256').update(s,'utf8').digest('hex');
const model={provider:'openrouter',openaiModel:'',reasoningEffort:'medium',revision:0};
const vectors=[];
function add(type,kind,name,patch={},extra={}){
 const input=type==='resume'?{resumeMd:'CI synthetic resume 😀',revision:0}:type==='prompt'?{markdown:'CI synthetic prompt 😀',revision:0}:{...model};
 vectors.push({type,...(kind?{kind}:{}),name,input:{...input,...patch},...extra});
}
for(const type of ['resume','prompt']){
 const field=type==='resume'?'resumeMd':'markdown';const groups=type==='resume'?['']:kinds;
 for(const kind of groups){
  const max=type==='resume'?20000:({cover_letter:15000,relevance:5000,vacancy_extraction:10000,vacancy_role:5000}[kind]);
  add(type,kind,'base');add(type,kind,'clear',{[field]:''});add(type,kind,'no-trim',{[field]:' \uFEFF CI синтетический <&> 😀\n\u0085 '});
  for(const unit of ['x','😀'])for(const offset of [0,1])add(type,kind,`${unit==='x'?'ascii':'emoji'}-${offset?'over':'at'}-limit`,{}, {textSpec:{field,unit,count:max+offset}});
  add(type,kind,'nul',{[field]:'CI\u0000text'});add(type,kind,'null-text',{[field]:null});add(type,kind,'number-text',{[field]:7});add(type,kind,'owner-field',{userName:'Nastya'});add(type,kind,'uppercase-field',{}, {raw:JSON.stringify({[field==='resumeMd'?'ResumeMd':'Markdown']:'CI',revision:0})});
  for(const revision of [null,-1,1.5,'0',true,9007199254740992])add(type,kind,'bad-revision-'+String(revision),{revision});
  add(type,kind,'missing-revision',{}, {raw:JSON.stringify({[field]:'CI'})});add(type,kind,'max-safe-revision',{revision:9007199254740991});add(type,kind,'decimal-integral-revision',{}, {raw:`{"${field}":"CI","revision":1.0}`});add(type,kind,'negative-zero-revision',{}, {raw:`{"${field}":"CI","revision":-0}`});
  add(type,kind,'last-duplicate-wins',{}, {raw:`{"${field}":"first","revision":1,"${field}":"second","revision":2}`});
  add(type,kind,'document-bom',{}, {prefixBOM:true});add(type,kind,'array',{}, {raw:'[]'});add(type,kind,'null',{}, {raw:'null'});add(type,kind,'trailing',{}, {raw:'{} {}'});add(type,kind,'no-body',{}, {noBody:true});
  const bytes=type==='resume'?256*1024:max>=10000?128*1024:32*1024;
  add(type,kind,'declared-too-large',{}, {declaredLength:bytes+1});add(type,kind,'stream-too-large',{}, {bodyRepeat:bytes+1});
 }
}
for(const kind of kinds){
 add('model',kind,'base');for(const provider of ['openrouter','openai'])add('model',kind,'provider-'+provider,{provider});
 for(const reasoningEffort of ['auto','none','minimal','low','medium','high','xhigh','max'])add('model',kind,'effort-'+reasoningEffort,{reasoningEffort});
 for(const openaiModel of ['gpt-6-luna','CI_model:1.test-a','0model','x'.repeat(160),'x'.repeat(161),' CI model','модель'])add('model',kind,'model-syntax-'+openaiModel.slice(0,12),{openaiModel});
 for(const patch of [{provider:'CI_unknown'},{provider:null},{openaiModel:null},{reasoningEffort:'CI_unknown'},{revision:null},{revision:9007199254740992},{revision:-1},{userName:'Nastya'},{openrouterModel:'jev'},{openrouterModel:'best_free'},{openrouterModel:null},{openrouterModel:''}])add('model',kind,'patch-'+Object.keys(patch)[0]+'-'+String(Object.values(patch)[0]),patch);
 add('model',kind,'declared-too-large',{}, {declaredLength:4097});add('model',kind,'stream-too-large',{}, {bodyRepeat:4097});
}
const source=await readFile(new URL('../server/routes/index.js',import.meta.url),'utf8');
const guardStart=source.indexOf("if(input.openrouterModel==='jev'&&!['relevance','vacancy_role'].includes(kind))return jsonResponse");
const guardEnd=source.indexOf("        if(input.provider==='openrouter'",guardStart);
if(guardStart<0||guardEnd<guardStart)throw new Error('Legacy JEV guard boundaries changed');
const modelGuard=new Function('input','kind','jsonResponse',source.slice(guardStart,guardEnd)+'return null;');
function bodyOf(v){
 if(v.bodyRepeat)return ' '.repeat(v.bodyRepeat);
 if(v.raw!==undefined)return v.raw;
 const input={...v.input};if(v.textSpec){const s=v.textSpec;input[s.field]=s.unit.repeat(s.count)+(s.suffix||'');}
 return (v.prefixBOM?'\uFEFF':'')+JSON.stringify(input);
}
for(const v of vectors){
 const body=bodyOf(v),headers={};if(v.declaredLength!==undefined)headers['content-length']=String(v.declaredLength);
 const request=new Request('https://itmap.test/profile',{method:'PUT',headers,body:v.noBody?undefined:body});
 try{
  const result=await(v.type==='resume'?readResumeInput(request):v.type==='prompt'?readPromptInput(request,v.kind):readPromptModelInput(request));
  if(v.type==='model'){
   const denied=modelGuard(result,v.kind,(_body,status)=>({status}));if(denied){v.status=denied.status;continue;}
   v.normalized={provider:result.provider,openaiModel:result.openaiModel,openrouterModel:result.openrouterModel,reasoningEffort:result.reasoningEffort,revision:result.revision};
  }else v.normalized={textHash:hash(result[v.type==='resume'?'resumeMd':'markdown']),revision:result.revision};
  v.status=200;
 }catch(error){v.status=error.status||400;}
}
function spyDB(){
 const calls=[],records=[];
 const row={id:1,kind:'relevance',markdown:'CI synthetic stored prompt',prompt_revision:1,model_revision:1,provider:'openrouter',openai_model:'',openrouter_model:'best_free',reasoning_effort:'medium',revision:1,updated_at:'2026-10-26 00:00:00'};
 return {calls,records,prepare(sql){calls.push(sql);const record={sql,args:[]};records.push(record);return {bind(...args){record.args=args;return this;},async run(){return {success:true};},async all(){return {results:[]};},async first(){return /RETURNING/.test(sql)?row:null;}};}};
}
const defaults={resume:await legacyReadResume(spyDB(),'Demo'),prompts:await readSystemPrompts(spyDB()),models:await readPromptModels(spyDB())};
const mappings=[];
for(const kind of kinds){
 const parts={};
 for(const part of ['prompt','model']){
  const db=spyDB();if(part==='prompt')await saveSystemPrompt(db,kind,{markdown:'CI synthetic prompt',revision:0});else await savePromptModel(db,kind,{...model,openrouterModel:'best_free'});
  const mutation=db.records.filter(record=>/RETURNING/.test(record.sql)).at(-1),sql=mutation?.sql;
  const table=sql?.match(/(?:INSERT INTO|UPDATE)\s+([a-z_]+)/)?.[1];
  const revisionColumn=sql?.match(/\b(prompt_revision|model_revision|revision)\s*=\s*(?:[a-z_]+\.)?\1\s*\+\s*1/)?.[1];
  if(!table||!revisionColumn)throw new Error('Cannot derive legacy storage mapping');
  parts[part]={table,keyColumn:sql.includes('WHERE id=1')?'id':'kind',keyValue:sql.includes('WHERE id=1')?1:mutation.args[0],revisionColumn};
 }
 mappings.push({kind,...parts});
}
const segments={
 resume:['    if (url.pathname === "/api/profile/resume") {',"\n    if(url.pathname==='/api/profile/prompt-models'"],
 models:["    if(url.pathname==='/api/profile/prompt-models'",'\n    if (url.pathname === "/api/profile/prompts"'],
 prompts:['    if (url.pathname === "/api/profile/prompts"',"\n    if(url.pathname==='/api/scheduler'"],
};
const AsyncFunction=Object.getPrototypeOf(async function(){}).constructor;
const names=['request','url','env','currentUser','jsonResponse','readUserResume','saveUserResume','readResumeInput','readPromptModels','readPromptModelInput','PROMPT_KINDS','generationBoundDb','ensureRuntimeGeneration','jevCatalogModel','validateOpenaiChoice','openaiModels','savePromptModel','readSystemPrompts','saveSystemPrompt','readPromptInput'];
const routes={};
for(const [surface,[start,end]]of Object.entries(segments)){const a=source.indexOf(start),b=source.indexOf(end,a+start.length);if(a<0||b<a)throw new Error('Legacy profile route boundaries changed');routes[surface]=new AsyncFunction(...names,source.slice(a,b));}
const permissions=[];
for(const surface of ['resume','prompts','models'])for(const user of [{name:'Eugene',isAdmin:true},{name:'Demo',isAdmin:false},{name:'Nastya',isAdmin:false}])for(const kind of surface==='resume'?['']:['',...kinds,'CI_unknown'])for(const method of ['GET','PUT','POST']){
 permissions.push({surface,kind,method,user:user.name,admin:user.isAdmin,storage:true});
}
for(const surface of ['resume','prompts','models'])for(const admin of [true,false])for(const method of ['GET','PUT','HEAD'])permissions.push({surface,kind:'',method,user:admin?'Eugene':'Demo',admin,storage:false});
for(const p of permissions){
 const suffix=p.surface==='resume'?'resume':p.surface==='models'?'prompt-models':'prompts';const url=new URL('https://itmap.test/api/profile/'+suffix+(p.kind?'/'+p.kind:''));
 const request=new Request(url,{method:p.method,...(!['GET','HEAD'].includes(p.method)?{body:JSON.stringify(p.surface==='resume'?{resumeMd:'CI',revision:0}:p.surface==='prompts'?{markdown:'CI',revision:0}:{...model})}:{})});
 const result=await routes[p.surface](request,url,{DB:p.storage?{}:null},{name:p.user,isAdmin:p.admin},(body,status=200)=>({body,status}),async(_db,user)=>({owner:user}),async(_db,user)=>({owner:user}),readResumeInput,async()=>({}),readPromptModelInput,kinds,db=>db,async()=>'CI_SYNTHETIC_GENERATION',async()=>{throw new Error('Provider validation must not run in permission oracle');},()=>{},async()=>[],async()=>({}),async()=>({}),async()=>({}),readPromptInput);
 if(!result)throw new Error('Permission oracle did not reach a legacy response');p.status=result.status;if(result.body.owner)p.owner=result.body.owner;
}
const schema=JSON.parse(await readFile(new URL('../server/generated/backup-schema.json',import.meta.url),'utf8'));
const selected=['user_profiles','system_prompts','prompt_model_settings','vacancy_detail_settings','vacancy_role_settings'];
const tables=selected.map(name=>{const t=schema.tables.find(t=>t.name===name);if(!t)throw new Error('Missing profile backup table');return t;});
const artifacts=[
 ['../internal/profiles/testdata/input-contract.json',{oracle:'Unchanged legacy streamed input readers, JEV route guard, read/save projections and authenticated route blocks. Synthetic requests and DB spies only; no SQL/providers.',vectors,mappings,permissions}],
 ['../internal/profiles/defaults.json',defaults],
 ['../internal/profiles/schema.json',{version:39,tables}],
];
const write=process.argv.length===3&&process.argv[2]==='--write';if(!write&&process.argv.length!==2)throw new Error('Use --write only for reviewed fixture regeneration');
for(const [path,data]of artifacts){const target=new URL(path,import.meta.url),content=JSON.stringify(data,null,2)+'\n';if(write)await writeFile(target,content);else if(await readFile(target,'utf8')!==content)throw new Error('Legacy profile oracle differs: '+path);}
console.log(`PASS: ${vectors.length} input vectors, ${permissions.length} route cases, ${mappings.length} storage mappings, ${tables.length} v39 tables; no SQL/network`);
