import {rabotaBodyPresent} from '../server/services/rabota-detail.js';
const cases=[
 ['empty',''],['title','# Senior CI'],['short','Обязанности: писать код'],
 ['russian','Обязанности: '+ 'Писать код. '.repeat(30)],['english',"Responsibilities: "+'Build services. '.repeat(30)],
 ['no_evidence','Условия: '+'Оплачиваемый отпуск. '.repeat(30)],
 ['footer_only','Смотрите также\nОбязанности: '+'Писать код. '.repeat(40)],
 ['footer_heading','### Смотрите также\nОбязанности: '+'Писать код. '.repeat(40)],
 ['contact_only','Задайте вопрос работодателю\nОбязанности: '+'Писать код. '.repeat(40)],
 ['links','Обязанности: https://example.com/'+'a'.repeat(280)],
 ['emoji_accepted','Задачи '+'😀'.repeat(123)],['emoji_rejected','Задачи '+'😀'.repeat(120)],
 ['newline_dot',"What you\nll do "+'CI DATA '.repeat(40)],['separator_dot',"What you\u2028ll do "+'CI DATA '.repeat(40)],
 ['long_s',"Reſponſibilitieſ "+'CI DATA '.repeat(40)],
 ['nbsp_heading','##\u00a0Смотрите также\nОбязанности: '+'Писать код. '.repeat(40)]
];
console.log(JSON.stringify(cases.map(([name,markdown])=>({name,markdown,expected:rabotaBodyPresent(markdown)}))));
