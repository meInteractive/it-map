import {sameVacancyPublication} from '../server/services/vacancy-duplicate-match.js';
import {sameHhPublication} from '../server/services/vacancy-source-priority.js';
const words='Управление командой планирование бюджета развитие инженеров техническая архитектура распределенных систем надежность платформы координация разработки оценка результатов продуктовое взаимодействие приоритеты риски качество релизов найм сотрудников облачная инфраструктура обратная связь профессиональный рост процессы поставки программного обеспечения';
const tasks=`${words} ${words}`;
const req='Знание современных инструментов разработка архитектуры ответственность коммуникация управление изменениями опыт проектирования распределенных сервисов';
const body=`# Head of Engineering\n## Задачи\n${tasks}\n## Требования\n${req}\n## Условия\nУдаленная работа`;
const cases=[
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering',otherBody:body,url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering Platform',otherBody:body.replace('## Задачи','## Основные задачи'),url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering Platform',otherBody:body.replace('## Задачи','## Задачи??'),url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering Platform',otherBody:'1 января 2026\n'+body,url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Junior Analyst',otherBody:body,url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering',otherBody:body+' truncated for model',url:'https://company.example/jobs/2'},
 {title:'Head of Engineering',body,otherTitle:'Head of Engineering',otherBody:body,url:'https://hh.ru/vacancy/11'},
 {title:'Head of Engineering',body:'short',otherTitle:'Head of Engineering',otherBody:'short',url:'https://company.example/jobs/2'},
];
for(const x of cases){const main={company_name:'CI',entity_type:'company',url:x.url,title:x.otherTitle,description_md:x.otherBody};const candidate={vacancyUrl:'https://hh.ru/vacancy/10'};const data={title:x.title,description_md:x.body};x.expected=sameHhPublication(main,candidate,data,'CI')||sameVacancyPublication(main,candidate,data,'CI');}
console.log(JSON.stringify(cases));
