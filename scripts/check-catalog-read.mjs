// Pure projection oracle: unchanged source functions + explicit read-set stubs.
// Identity INSERTs are observed but NOT simulated. Actual SQL/ID bootstrap and
// application-winner queries belong to GO-317, with preallocated IDs.
import {readCompactEntities,readCompanyDetail} from '../server/services/catalog-projection.js';
globalThis.fetch=()=>{throw new Error('Catalog projection oracle forbids network');};
const base=()=>({companies:[{name:'CI B',sector:'CI sector',jobs:[],offices:[]},{name:'CI A',offices:[{metro:'CI метро',address:'CI <&> \u2028😀',lat:55.5,lon:37.5}],jobs:[{title:'CI developer',url:'https://example.invalid/job'}],ratingInputs:{roleFit:7},middle:['100','200'],deferment:null,privateDetail:'CI detail only'}],agencies:[{name:'CI agency',rank:7,offices:[{}],jobs:[],functionalSpecializations:['IT'],middle:[100,200]}]});
const states=[
 {identities:[],vacancies:[],applications:[],initialized:[]},
 {identities:[{id:11,name:'CI A',entityType:'company',favorite:true,hidden:false},{id:12,name:'CI B',entityType:'company',favorite:false,hidden:true},{id:21,name:'CI agency',entityType:'agency',favorite:true,hidden:true}],vacancies:[],applications:[{name:'CI A',status:''},{name:'CI B',status:'Отказ'}],initialized:[]},
 {identities:[],vacancies:['CI B'],applications:[{name:'CI agency',status:'CI status'}],initialized:['CI A','CI agency']},
 {identities:[{id:11,name:'CI A',entityType:'company',favorite:true,hidden:false}],vacancies:['CI A'],applications:[],initialized:[]}
];
const variants=[],add=(name,mutate)=>{const c=base();mutate(c);variants.push({name,raw:JSON.stringify(c)});};
add('base',()=>{});
for(const offices of [null,false,0,'',{},'CI string',[{}],[{address:null,metro:false,lat:0,lon:null}],[0,'CI',false], [null]])add('offices '+JSON.stringify(offices),c=>{c.companies[1].offices=offices;});
for(const jobs of [null,false,0,'',{},'CI string',[{}],[{title:' ',url:'https://example.invalid'}],[{title:1,url:['https://example.invalid']}],[{title:0,url:'https://example.invalid'}],[{title:'CI',url:'HTTP://example.invalid'}],[{title:'CI',url:{toString:null}}],[{title:'CI',url:'https://example.invalid'},null],[null]])add('jobs '+JSON.stringify(jobs),c=>{c.companies[1].jobs=jobs;});
for(const value of [null,false,0,'',{},['CI']])add('deferment '+JSON.stringify(value),c=>{c.companies[1].deferment=value;});
add('raw property types',c=>Object.assign(c.companies[1],{offices:[{address:17,metro:['CI'],lat:false,lon:{CI:1}}],middle:[false,'0x100'],ratingFormulaVersion:-1,rank:999,vacancySources:{hh:'  https://example.invalid  '}}));
add('Unicode and stable ties',c=>{c.companies=['Я','A','Й','И\u0306','é','e\u0301','CI\ud800','CI�','FDD1'].map(name=>({name}));});
add('duplicate names',c=>c.companies.push({...c.companies[1],marker:'CI later duplicate'}));
add('agency filtering and coercion',c=>{c.agencies.push(null,{name:' '},{name:'CI second agency',rank:'3',revenue:{amount:'250'},middle:['10','20'],functionalSpecializations:'IT; CI'},false);});
add('prototype company names',c=>{c.companies=['__proto__','constructor','toString'].map(name=>({name}));});
add('source scope privacy',c=>Object.assign(c.companies[1],{reviews:{plus:'CI private detail'},resume:'CI never compact',revenue:{legal:'CI legal'},jobs:[{title:'CI title',url:'https://example.invalid',description:'CI never compact'}]}));
for(const raw of ['', 'null','{}','[]','true','{"companies":[]}','{"companies":[{"name":" "}]}','{"companies":[{"name":1}]}','CI invalid JSON','{"companies":[{"name":"CI A","jobs":[{"title":1e400,"url":"https://example.invalid"}]}]}','{"companies":[{"name":"CI A","offices":[{"lat":1e400}]}]}'])variants.push({name:'raw '+raw,raw});
async function reference(v){let writes=0;
 const db={prepare(sql){let args=[];const statement={bind(...a){args=a;return this;},async first(){
  if(sql.includes('FROM app_catalog'))return {dataJson:v.raw};
  if(sql.includes('FROM company_identity WHERE id=')||sql.includes('FROM agency_identity WHERE id=')){const type=sql.includes('agency_identity')?'agency':'company';const row=v.state.identities.find(s=>s.entityType===type&&s.id===args[0]);return row?{name:row.name}:null;}
  if(sql.includes('FROM company_user_state'))return {favorite:Number(v.options.favorite),hidden:Number(v.options.hidden)};
  if(sql.includes('COUNT(*) AS total FROM company_vacancies'))return {total:v.options.vacancyCount};
  throw new Error('Unreviewed source first SQL');
 },async run(){
  if(sql.startsWith('INSERT OR IGNORE INTO company_identity')||sql.startsWith('INSERT OR IGNORE INTO agency_identity')){writes++;return {meta:{changes:0}};}
  if(sql.includes('UNION ALL SELECT i.id,i.agency_name'))return {results:v.state.identities.map(r=>({...r,favorite:Number(r.favorite),hidden:Number(r.hidden)}))};
  if(sql.startsWith('SELECT company_name AS name FROM company_vacancies UNION'))return {results:v.state.vacancies.map(name=>({name}))};
  if(sql.includes('ROW_NUMBER() OVER(PARTITION BY company_name'))return {results:v.state.applications};
  if(sql.startsWith('SELECT company_name AS name FROM company_vacancy_state'))return {results:v.state.initialized.map(name=>({name}))};
  throw new Error('Unreviewed source projection SQL');
 }};return statement;},async batch(statements){const out=[];for(const s of statements)out.push(await s.run());return out;}};
 try{const o=v.options,value=o.view==='detail'?await readCompanyDetail(db,'CI user',o.id,o.name,o.entityType):await readCompactEntities(db,'CI user',o.view,o.entityType);return {...v,body:JSON.stringify(value),error:false,sourceIdentityBootstrapCalls:writes};}
 catch(e){if(!(e instanceof TypeError)&&!(e instanceof SyntaxError))throw e;return {...v,body:'',error:true,sourceIdentityBootstrapCalls:writes};}
}
let count=0;
for(const variant of variants)for(let si=0;si<states.length;si++)for(const entityType of ['company','agency'])for(const view of ['list','map','detail']){
 const options={entityType,view,id:0,name:entityType==='company'?'CI A':'CI agency',favorite:si===1,hidden:si===2,vacancyCount:si+1};
 process.stdout.write(JSON.stringify(await reference({name:variant.name+'/'+si+'/'+entityType+'/'+view,raw:variant.raw,state:states[si],options}))+'\n');count++;
}
for(const options of [{entityType:'company',view:'detail',id:11,name:'CI absent',favorite:true,hidden:false,vacancyCount:0},{entityType:'agency',view:'detail',id:21,name:'CI absent',favorite:false,hidden:true,vacancyCount:8},{entityType:'company',view:'detail',id:999,name:'CI A',favorite:false,hidden:false,vacancyCount:0},{entityType:'company',view:'detail',id:0,name:'CI absent',favorite:false,hidden:false,vacancyCount:0}]){process.stdout.write(JSON.stringify(await reference({name:'detail resolution '+options.id+'/'+options.entityType,raw:JSON.stringify(base()),state:states[1],options}))+'\n');count++;}
process.stderr.write(`Catalog source projection fixtures: ${count}; read-set stubs only, no SQL/database acceptance\n`);
