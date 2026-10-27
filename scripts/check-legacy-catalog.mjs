// Synthetic entity normalization oracle; no sorting substitute, SQL or network.
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync} from 'node:fs';
import * as constants from '../server/constants.js';
import {DATA} from '../server/data.js';
import {normalizeCatalog,normalizeAgency} from '../server/services/catalog.js';
globalThis.fetch=()=>{throw new Error('Legacy catalog oracle forbids network');};
const optionNames={agencyTypes:'AGENCY_TYPE_OPTIONS',functionalSpecializations:'AGENCY_FUNCTION_OPTIONS',positionLevels:'AGENCY_POSITION_LEVEL_OPTIONS',executiveSearch:'AGENCY_EXECUTIVE_SEARCH_OPTIONS',closedVacancies:'AGENCY_CLOSED_VACANCIES_OPTIONS',vacancyPublicity:'AGENCY_VACANCY_PUBLICITY_OPTIONS',employerDisclosure:'AGENCY_EMPLOYER_DISCLOSURE_OPTIONS',salaryDisclosure:'AGENCY_SALARY_DISCLOSURE_OPTIONS',primaryVacancyChannel:'AGENCY_PRIMARY_CHANNEL_OPTIONS',publicSourcesQuality:'AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS',vacancyFreshness:'AGENCY_VACANCY_FRESHNESS_OPTIONS',acceptsCv:'AGENCY_ACCEPTS_CV_OPTIONS',recommendedContactMethods:'AGENCY_CONTACT_METHOD_OPTIONS'};
const policy={embeddedCompanies:DATA.companies,vacancyKeys:constants.VACANCY_SOURCE_META.map(([k])=>k),reviewKeys:constants.REVIEW_SOURCE_META.map(([k])=>k),agencyReviewKeys:constants.AGENCY_REVIEW_SOURCE_META.map(([k])=>k),vacancyDefaults:constants.COMPANY_VACANCY_SOURCE_URLS,applicationUrls:constants.COMPANY_APPLICATION_URLS,applicationEmails:constants.COMPANY_APPLICATION_EMAILS,options:Object.fromEntries(Object.entries(optionNames).map(([key,name])=>[key,constants[name]])),ratingFormulaVersion:constants.RATING_FORMULA_VERSION,wlbWeight:constants.WLB_RATING_WEIGHT};
const policyPath=new URL('../internal/v39/catalog_policy.json',import.meta.url),policyText=JSON.stringify(policy)+'\n';
if(process.argv.includes('--write-policy'))writeFileSync(policyPath,policyText);else assert.equal(readFileSync(policyPath,'utf8'),policyText,'Source catalog metadata drift');
const stdio=process.argv.includes('--stdio');let count=0,accepted=0;
function vector(kind,name,change,rawChange){let input={name:'CI Synthetic Entity',unknown:{keep:' 😀 \ud800 ',number:7},rank:99,score:77};change?.(input);const body=rawChange?.(JSON.stringify(input))??JSON.stringify(input),value=JSON.parse(body),before=JSON.stringify(value);let expected,ok=true;try{expected=kind==='company'?normalizeCatalog({companies:[value]}).companies[0]:normalizeAgency(value)}catch{ok=false};assert.equal(JSON.stringify(value),before,'Source mutated caller');count++;accepted+=Number(ok);if(stdio)process.stdout.write(JSON.stringify({name:`${kind} ${name}`,kind,body,ok,expected:ok?JSON.stringify(expected):null})+'\n');}
for(const kind of ['company','agency']){
 vector(kind,'missing defaults');
 for(const key of ['middle','senior','lead','head'])for(const value of [null,{},[],[0,1],['250000','300001'],[true,false],['0xA',''],[9007199254740991,9007199254740992],[-1,2],[1.5,2],[[5],null]])vector(kind,`salary ${key} ${JSON.stringify(value)}`,p=>p[key]=value);
 for(const value of [null,{},[],{plus:' \uFEFF CI 😀 \ud800 ',minusShort:'   ',minus:5,plusShort:null},'ignored'])vector(kind,`reviews ${JSON.stringify(value)}`,p=>p.reviews=value);
 for(const key of ['vacancySources','reviewSources'])for(const value of [null,[],{},'ignored',{hh:' CI ',companySite:' \u0085 ',dreamJob:'',habr:false,glassDoor:' \uFEFF CI '}])vector(kind,`sources ${key} ${JSON.stringify(value)}`,p=>p[key]=value);
 for(const key of ['applicationUrl','applicationEmail'])for(const value of [null,5,' \uFEFF CI \ud800 ','\u0085','   '])vector(kind,`${key} ${JSON.stringify(value)}`,p=>p[key]=value);
 vector(kind,'numeric own keys and overridden output fields',p=>Object.assign(p,{'10':'ten','2':'two',entityType:'CI old',rank:20,score:-7}));
 for(const key of ['middle','rating','rank','score','ratingInputs','wlbScore','roles','revenue'])vector(kind,`throwing toString ${key}`,p=>p[key]={toString:5,valueOf:7});
 vector(kind,'overflow arithmetic must precede stringify',p=>Object.assign(p,{rating:'CI_INF',ratingInputs:{roleFit:'CI_INF',deferment:'CI_INF',wlbScore:'CI_INF'},wlbScore:'CI_INF',ratingFormulaVersion:'CI_INF',revenue:{amount:'CI_INF',year:'CI_INF',metric:['CI_INF']},score:'CI_INF'}),s=>s.replaceAll('"CI_INF"','1e400'));
}
for(const field of ['roles','deferment','mobilization','rating','ratingFormulaVersion','wlbScore'])for(const value of [null,'',false,0,1,'4',5,7,10,'подтверждена аккредитация','не подтверждено','аккредитация уточнить','Product Project Program Delivery Development Engineering разработка технический',[],[5],{value:7}])vector('company',`${field} ${JSON.stringify(value)}`,p=>p[field]=value);
for(const field of ['roleFit','deferment','wlbScore'])for(const value of [null,0,0.49999999999999994,0.5,'',false,'5',20,50,100,{},[5]])vector('company',`inputs ${field} ${JSON.stringify(value)}`,p=>p.ratingInputs={[field]:value});
for(const formula of [0,1,2,3,4,5])for(const wlb of [null,0,8,10.666666666666666,11,15,20,50,100])vector('company',`wlb legacy formula ${formula} stored ${wlb}`,p=>Object.assign(p,{ratingFormulaVersion:formula,wlbScore:wlb,ratingInputs:{wlbScore:8}}));
for(const amount of [null,0,1,1e9-1,1e9,1e10,5e10,1e11,5e11,1e12,'1000000000000'])vector('company',`revenue ${amount}`,p=>p.revenue={amount});
for(const score of [null,0,5,10,'5',15])for(const evidence of ['', ' CI ', '\uFEFF', '\u0085'])vector('company',`AI ${score} ${JSON.stringify(evidence)}`,p=>Object.assign(p,{aiScore:score,aiEvidence:evidence}));
for(const embedded of DATA.companies){vector('company',`embedded defaults ${embedded.name}`,p=>p.name=embedded.name);vector('company',`embedded override ${embedded.name}`,p=>Object.assign(p,{name:embedded.name,ratingInputs:{roleFit:null,wlbScore:0},reviewSources:{habr:' CI override '},reviews:{plus:' CI override '},applicationEmail:' CI override '}));}
for(const name of ['__proto__','constructor','toString','toLocaleString','valueOf','hasOwnProperty','isPrototypeOf','propertyIsEnumerable','__defineGetter__','__defineSetter__','__lookupGetter__','__lookupSetter__'])for(const provided of [false,true])vector('company',`prototype name ${name} provided ${provided}`,p=>{p.name=name;if(provided){p.applicationUrl=' CI override ';p.applicationEmail=7;}});
for(const embedded of DATA.companies)for(const value of [[],['CI index'],[1,2],{unknown:'CI',roleFit:0}])vector('company',`embedded numeric object spread ${embedded.name} ${JSON.stringify(value)}`,p=>{p.name=embedded.name;p.ratingInputs=value;});
for(const name of ['CI \ud800','CI \udfff','CI \u2028😀','Пример работодателя 1 '])vector('company',`exact UTF16 name ${JSON.stringify(name)}`,p=>p.name=name);
for(const name of Object.keys(constants.COMPANY_VACANCY_SOURCE_URLS))vector('company',`public defaults ${name}`,p=>p.name=name);
for(const [field,options] of Object.entries(policy.options)){
 const many=['agencyTypes','functionalSpecializations','positionLevels','recommendedContactMethods'].includes(field);
 for(const value of [null,'',options[0].toUpperCase(),` ${options[0].toLowerCase()} `,`${options[0]};${options.at(-1)};${options[0]}`,[options[0].toLowerCase(),options.at(-1),options[0],null,7],{},false])vector('agency',`choice ${field} ${JSON.stringify(value)}`,p=>p[field]=value);
}
for(const [key] of constants.AGENCY_RATING_INPUT_META)for(const value of [null,0,'',true,false,'0xA',5.5000000001,5.25,[5],{},'Infinity'])vector('agency',`rating ${key} ${JSON.stringify(value)}`,p=>p[key]=value);
for(const field of ['rank','score','reviewsScore','relevanceScore','revenueScore'])for(const value of [null,0,'',1,'5.5',80,100,101,'0xA',true,[5],{},'Infinity'])vector('agency',`number ${field} ${JSON.stringify(value)}`,p=>p[field]=value);
for(const value of [null,{},[],{middle:'vacancies',senior:'manual',head:'bad'},'ignored'])vector('agency',`salary sources ${JSON.stringify(value)}`,p=>p.salaryRangeSource=value);
for(const value of [null,{},[],{amount:0,year:'CI year',metric:7,legal:[' CI '],inn:null,source:' CI fallback ',source1:'',source2:false,comment:{valueOf:1}}])vector('agency',`revenue fields ${JSON.stringify(value)}`,p=>p.revenue=value);
for(const value of [null,5,'CI',' \uFEFF '+'😀'.repeat(256)+'more','x'.repeat(511)+'😀'])vector('agency',`guide UTF16 ${typeof value} ${String(value).length}`,p=>p.workingGuide=value);
for(const value of [null,{},'ignored',[{url:'CI',title:'CI'}]])vector('agency',`jobs ${JSON.stringify(value)}`,p=>p.jobs=value);
if(!stdio)console.log(`PASS: ${count} unchanged entity normalization vectors (${accepted} accepted); sorting/legacy plan remains separate; no SQL/network`);
