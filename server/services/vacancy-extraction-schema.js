import {VACANCY_LEVELS} from '../constants.js';
export const EXTRACTION_SCHEMA_VERSION=2;
export const EXTRACTION_INPUT_LIMIT=120000;
export {DEFAULT_EXTRACTION_PROMPT} from './vacancy-extraction-prompt.js';
const nullableString={type:['string','null']};
const nullableEnum=values=>({type:['string','null'],enum:[...values,null]});
export const EXTRACTION_SCHEMA={type:'object',additionalProperties:false,properties:{
 title:{type:'string'},employer_name:nullableString,location:nullableString,
 work_format:nullableEnum(['office','hybrid','remote']),employment_type:nullableString,level:{type:'string',enum:VACANCY_LEVELS,description:'Обязательная оценка уровня: Middle, Senior, Lead или Head. При отсутствии прямого грейда определи по обязанностям и масштабу ответственности согласно системному промту; null запрещён.'},
 salary_from:{type:['number','null']},salary_to:{type:['number','null']},salary_currency:nullableString,
 salary_period:nullableEnum(['hour','month','year','other']),salary_tax_type:nullableEnum(['gross','net']),
 responsibilities:nullableString,requirements:nullableString,conditions:nullableString,published_at:{type:['string','null'],pattern:'^[0-9]{4}-(0[1-9]|1[0-2])-(0[1-9]|[12][0-9]|3[01])$',description:'Дата публикации в формате YYYY-MM-DD, только если год, месяц и день подтверждены исходным текстом. Неизвестная, неполная или относительная дата без опорной даты: null. Не используй дату загрузки.'},
 description_md:{type:'string'},language:nullableString
},required:['title','employer_name','location','work_format','employment_type','level','salary_from','salary_to','salary_currency','salary_period','salary_tax_type','responsibilities','requirements','conditions','published_at','description_md','language']};
const invalid=message=>Object.assign(new Error(message),{code:'invalid_output',status:422});
export function validateVacancyExtraction(content,raw,{corrections}={}){
 const unverified=(key,message)=>{if(!Array.isArray(corrections))throw invalid(message);corrections.push({field:key,reason:message});value[key]=null;};
 let value;try{value=JSON.parse(content);}catch{throw invalid('Модель вернула некорректный JSON.');}
 if(!value||Array.isArray(value)||typeof value!=='object'||Object.keys(value).length!==EXTRACTION_SCHEMA.required.length||Object.keys(value).some(k=>!EXTRACTION_SCHEMA.required.includes(k)))throw invalid('Поля ответа не соответствуют схеме.');
 for(const [k,s] of Object.entries(EXTRACTION_SCHEMA.properties)){
  const v=value[k],types=Array.isArray(s.type)?s.type:[s.type];
  if(!types.includes(v===null?'null':typeof v)||typeof v==='number'&&(!Number.isFinite(v)||v<0)||typeof v==='string'&&(!v.trim()||v.includes('\0')||v.length>150000)||s.enum&&!s.enum.includes(v))throw invalid('Некорректное поле '+k+'.');
 }
 if(value.title.length>500||value.description_md.trim().length<100)throw invalid('Недостаточное содержательное описание.');
 if(value.salary_from!==null&&value.salary_to!==null&&value.salary_from>value.salary_to)throw invalid('Границы зарплаты перепутаны.');
 const normalized=raw.toLowerCase().replace(/(?<=\d)[\s\u00a0]+(?=\d)/g,''),salaryText=normalized.match(/[^\n]{0,150}(?:зарплат|оклад|salary|руб|доллар|евро|₽|\$|usd|eur|€)[^\n]{0,150}/gi)?.join('\n')||'';
 for(const k of ['salary_from','salary_to'])if(value[k]!==null){
  const n=value[k],literal=String(n).replace(/[.*+?^${}()|[\]\\]/g,'\\$&'),short=String(n/1000).replace('.', '[.,]');
  if(!new RegExp('(?:^|[^0-9])'+literal+'(?![0-9])').test(salaryText)&&!new RegExp('(?:^|[^0-9])'+short+'\\s*(?:тыс|k\\b)').test(salaryText))throw invalid('Зарплата не подтверждается исходным текстом.');
 }
 const evidence={salary_currency:{RUB:/руб|₽|\brub\b/i,USD:/доллар|\$|\busd\b/i,EUR:/евро|€|\beur\b/i},salary_tax_type:{gross:/до (?:вычета )?налог|gross|before tax/i,net:/на руки|после (?:вычета )?налог|net|after tax/i},salary_period:{month:/месяц|month|\/мес/i,year:/год|year|annual/i,hour:/час|hour|hourly/i},work_format:{office:/офис|office|on.site/i,hybrid:/гибрид|hybrid/i,remote:/удал[её]н|remote|дистанцион/i}};
 for(const [key,options] of Object.entries(evidence))if(value[key]!==null){const pattern=options[value[key]];if(pattern&&!pattern.test(raw))unverified(key,'Значение '+key+' не подтверждается исходным текстом.');else if(key==='salary_currency'&&!pattern&&!raw.toLowerCase().includes(value[key].toLowerCase()))unverified(key,'Валюта не подтверждена.');}
 if(value.published_at!==null){const date=value.published_at;if(!/^\d{4}-\d{2}-\d{2}$/.test(date)||!Number.isFinite(Date.parse(date))||new Date(date).toISOString().slice(0,10)!==date)throw invalid('Дата публикации должна быть действительной датой YYYY-MM-DD.');const [y,m,d]=date.split('-'),months=['январ','феврал','март','апрел','мая','июн','июл','август','сентябр','октябр','ноябр','декабр'];if(!raw.includes(date)&&!raw.includes(d+'.'+m+'.'+y)&&!new RegExp('\\b'+Number(d)+'\\s+'+months[Number(m)-1]+'[^\\n]{0,12}'+y,'i').test(raw))unverified('published_at','Дата публикации не подтверждается источником.');}
 return value;
}

// Optional unsupported metadata is omitted, never inferred. Malformed JSON,
// required text and unsupported salary AMOUNTS remain hard errors.
export function normalizeVacancyExtraction(content,raw){
 const corrections=[];const data=validateVacancyExtraction(content,raw,{corrections});return {data,corrections};
}
