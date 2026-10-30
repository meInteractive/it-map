import {EXTRACTION_SCHEMA,EXTRACTION_SCHEMA_VERSION,EXTRACTION_INPUT_LIMIT,normalizeVacancyExtraction,validateVacancyExtraction} from '../server/services/vacancy-extraction-schema.js';
const base=Object.fromEntries(EXTRACTION_SCHEMA.required.map(k=>[k,null]));Object.assign(base,{title:'CI_TITLE',level:'Senior',description_md:'CI_DESCRIPTION '.repeat(12)});
const cases=[
 ['basic',{},'CI_TEXT'],
 ['salary_long_s',{salary_from:100000},'ſalary 100000'],
 ['currency_long_s',{salary_currency:'USD'},'uſd'],
 ['gross_long_s',{salary_tax_type:'gross'},'groſſ'],
 ['work_line_separator',{work_format:'office'},'on\u2028site'],
 ['date_utf16_window',{published_at:'2026-10-23'},'23 октябр'+'😀'.repeat(8)+'2026'],

 ['salary_nbsp_short',{salary_from:150000},'Зарплата 150\u00a0тыс руб'],
 ['date_nbsp',{published_at:'2026-10-23'},'Дата 23\u00a0октября 2026'],
 ['enum_object',{work_format:{}},'CI_TEXT'],['enum_array',{level:[]},'CI_TEXT'],
 ['currency_unicode',{salary_currency:'ΟΣ'},'Salary ος'],
 ['salary_utf16_context',{salary_from:100000},'Salary'+'😀'.repeat(140)+'100000'],
['salary',{salary_from:100000,salary_to:150000,salary_currency:'RUB',salary_period:'month',salary_tax_type:'net'},'Зарплата 100 000–150 000 руб в месяц на руки.'],
 ['salary_short',{salary_from:150000},'Salary 150k'],['salary_fraction',{salary_from:150500},'Зарплата 150,5 тыс руб'],['salary_unknown',{salary_from:100000},'CI_TEXT'],
 ['salary_reversed',{salary_from:200000,salary_to:100000},'Зарплата 100000–200000 руб'],['salary_negative',{salary_from:-1},'Зарплата -1 руб'],
 ['work_unknown',{work_format:'remote'},'CI_TEXT'],['work_known',{work_format:'remote'},'Удалённая работа.'],['gross_unknown',{salary_tax_type:'gross'},'CI_TEXT'],
 ['currency_unknown',{salary_currency:'USD'},'CI_TEXT'],['currency_custom',{salary_currency:'GBP'},'Salary GBP'],['period_other',{salary_period:'other'},'CI_TEXT'],
 ['date_iso',{published_at:'2026-10-23'},'Дата 2026-10-23'],['date_dots',{published_at:'2026-10-23'},'Дата 23.10.2026'],['date_ru',{published_at:'2026-10-23'},'Дата 23 октября 2026'],
 ['date_unknown',{published_at:'2026-10-23'},'Сегодня'],['date_invalid',{published_at:'2026-02-29'},'2026-02-29'],['date_shape',{published_at:'23.10.2026'},'23.10.2026'],
 ['title_empty',{title:' '},'CI_TEXT'],['description_short',{description_md:'short'},'CI_TEXT'],['level_null',{level:null},'CI_TEXT'],['level_invalid',{level:'Junior'},'CI_TEXT'],
 ['extra_field',{alien:true},'CI_TEXT'],['number_string',{salary_from:'100000'},'Зарплата 100000 руб'],['nullbyte',{requirements:'CI\0TEXT'},'CI_TEXT'],
 ['salary_no_boundary',{salary_from:100000},'Зарплата 21000001 руб'],['salary_other_line',{salary_from:100000},'Salary\nCI 100000'],
 ['salary_zero',{salary_from:0},'Зарплата 0 руб'],['title_utf16',{title:'😀'.repeat(251)},'CI_TEXT'],['work_unknown_strict',{work_format:'remote'},'CI_TEXT',true],
];
const frames=cases.map(([name,patch,raw,strict=false])=>{const content=JSON.stringify({...base,...patch});let expected;try{expected={ok:true,result:strict?{data:validateVacancyExtraction(content,raw),corrections:[]}:normalizeVacancyExtraction(content,raw)}}catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message}}return{name,content,raw,strict,...expected}});
for(const content of ['{','null','[]','{}']){let expected;try{expected={ok:true,result:normalizeVacancyExtraction(content,'CI_TEXT')}}catch(e){expected={ok:false,code:e.code,status:e.status,message:e.message}}frames.push({name:'json_'+content,content,raw:'CI_TEXT',...expected})}
console.log(JSON.stringify({schema:EXTRACTION_SCHEMA,version:EXTRACTION_SCHEMA_VERSION,limit:EXTRACTION_INPUT_LIMIT,frames}));
