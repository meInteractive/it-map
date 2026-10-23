import {decodeListingText} from './vacancy-listing-extractor.js';

export const RABOTA_MISSING_BODY='В карточке rabota.ru отсутствует содержательный текст обязанностей. Получены только заголовок и служебные блоки.';
export const incompleteRabotaBody=validation=>validation?.errors?.length===1&&validation.errors[0]===RABOTA_MISSING_BODY;

// Boilerplate (experience in the header, salary links in the footer) is not a
// job description. A missing body must stop before paid extraction/role choice.
export function rabotaBodyPresent(markdown){
 const body=String(markdown||'').split(/###?\s*Смотрите также|Задайте вопрос работодателю|Напишите работодателю свой телефон/i)[0];
 return /обязанност|(?:чем|что|вам) предстоит|вы будете|responsibilit|what you.ll do|задачи|отвечать за|в зону ответственности|развива(?:ть|ем) продукт|управл(?:ять|ение) продукт/i.test(body)&&body.replace(/https?:\/\/\S+/g,'').trim().length>=250;
}

export function rabotaDetailMarkdown(data,url){
 const markdown=typeof data.markdown==='string'?data.markdown:'';
 if(rabotaBodyPresent(markdown))return markdown;
 // Prefer the publisher's structured description for this exact numeric card.
 // Never read related-job objects or execute script content.
 const id=new URL(url).pathname.match(/^\/vacancy\/(\d+)\/?$/)?.[1];
 for(const script of String(data.html||'').matchAll(/<script\b[^>]*type=["']application\/ld\+json["'][^>]*>([\s\S]*?)<\/script>/gi)){
  let value;try{value=JSON.parse(script[1]);}catch{continue;}
  const nodes=Array.isArray(value)?value:[value,...(Array.isArray(value?.['@graph'])?value['@graph']:[])];
  const jobs=nodes.filter(v=>v&&[v['@type']].flat().includes('JobPosting'));
  for(const job of jobs){
   const linked=job.url??job['@id'],identifier=typeof job.identifier==='object'?job.identifier?.value:job.identifier;
   if(linked){try{if(new URL(linked,url).pathname.replace(/\/$/,'')!=='/vacancy/'+id)continue;}catch{continue;}}
   else if(jobs.length!==1||String(identifier)!==id)continue;
   if(typeof job.description!=='string'||typeof job.title!=='string')continue;
   const description=job.description.replace(/<\/(?:p|div|li|h[1-6])>|<br\s*\/?>/gi,'\n').split('\n').map(decodeListingText).filter(Boolean).join('\n\n');
   const text='# '+decodeListingText(job.title)+'\n\n'+description;
   if(rabotaBodyPresent(text))return text;
  }
 }
 return markdown;
}
