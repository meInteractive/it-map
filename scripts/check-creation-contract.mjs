// Synthetic legacy oracle only: no DB, network, providers or user data.
import {readFile, writeFile} from 'node:fs/promises';

globalThis.fetch = () => { throw new Error('Contract oracle must not use the network'); };
const {normalizeApplicationCreation, applicationCreationHash} = await import('../server/repositories/application-creation.js');
const base = {companyName:'CI Synthetic Employer'};
const cases = [
 ['defaults', {}],
 ['trim-and-source-set', {companyName:' CI Synthetic Employer ',recruiterName:' CI Synthetic Recruiter ',recruiterPhone:' +1-555-0100 ',recruiterEmail:' ci@example.invalid ',recruiterContactType:'direct',applicationSources:['telegram','hh','hh',' site ']}],
 ['unicode-json', {comment:'Тест 😀 <&> \u2028 \u2029 " \\ / \u0000\n',recruiterName:'Синтетический контакт'}],
 ['all-sources', {applicationSources:['site','hh','habr','email','telegram','hcm']}],
 ['null-defaults', {vacancyId:null,applicationSources:null}],
 ['linked-vacancy', {vacancyId:42,status:'Откликнулся'}],
 ['largest-safe-vacancy', {vacancyId:9007199254740991}],
 ['unsafe-vacancy', {vacancyId:9007199254740992}],
 ['empty-company', {companyName:' '}],
 ['unknown-status', {status:'CI INVALID STATUS'}],
 ['comment-limit', {comment:'😀'.repeat(500)}],
 ['comment-over-limit', {comment:'😀'.repeat(500)+'x'}],
 ['name-limit', {recruiterName:'😀'.repeat(80)}],
 ['name-over-limit', {recruiterName:'😀'.repeat(80)+'x'}],
 ['phone-limit', {recruiterPhone:'x'.repeat(64)}],
 ['phone-over-limit', {recruiterPhone:'x'.repeat(65)}],
 ['email-limit', {recruiterEmail:'x'.repeat(250)+'@a.b'}],
 ['email-over-limit', {recruiterEmail:'x'.repeat(251)+'@a.b'}],
 ['invalid-email', {recruiterEmail:'ci@@example.invalid'}],
 ['invalid-contact-type', {recruiterContactType:'CI UNKNOWN'}],
 ['empty-source-set', {applicationSources:[]}],
 ['unknown-source', {applicationSources:['CI UNKNOWN']}],
 ['bom-company', {companyName:'\uFEFFCI Synthetic Employer\uFEFF'}],
 ['only-bom-company', {companyName:'\uFEFF'}],
 ['nel-company-preserved', {companyName:'\u0085CI Synthetic Employer\u0085'}],
 ['bom-contacts', {recruiterName:'\uFEFFCI Synthetic Recruiter\uFEFF',recruiterPhone:'\uFEFF+1-555-0100\uFEFF',recruiterEmail:'\uFEFFci@example.invalid\uFEFF'}],
 ['nel-contact-preserved', {recruiterName:'\u0085CI Synthetic Recruiter\u0085'}],
 ['bom-source', {applicationSources:['\uFEFFhh\uFEFF']}],
 ['nel-source-invalid', {applicationSources:['\u0085hh\u0085']}],
 ['email-nbsp-invalid', {recruiterEmail:'ci\u00A0name@example.invalid'}],
 ['email-separator-invalid', {recruiterEmail:'ci\u2028name@example.invalid'}],
 ['email-bom-invalid', {recruiterEmail:'ci\uFEFFname@example.invalid'}],
 ['email-nel-preserved', {recruiterEmail:'ci\u0085name@example.invalid'}],
 ['ecmascript-whitespace', {companyName:'\u0009\u000B\u000C\u000D\u0020\u00A0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200A\u2028\u2029\u202F\u205F\u3000\uFEFFCI Synthetic Employer\n'}],
];
for (const status of ['', 'Откликнулся','Получил ответ','Скрининг','Собес 2','Собес 3','Собес 4','Офер','Отказ']) cases.push(['status-'+(status||'empty'),{status}]);
const vectors=[];
for (const [name,patch] of cases) {
 const input={...base,...patch};
 let result;
 try {
  const normalized=normalizeApplicationCreation(input);
  result={valid:true,normalized,hash:await applicationCreationHash(normalized)};
 } catch(error) {
  if (error.status!==400) throw error;
  result={valid:false,status:400};
 }
 vectors.push({name,input,...result});
}
const target=new URL('../internal/applications/testdata/creation-contract.json',import.meta.url);
const output=JSON.stringify({oracle:'Sites v316: normalizeApplicationCreation/applicationCreationHash; synthetic valid-Unicode typed inputs',vectors},null,2)+'\n';
if (process.argv.length===3 && process.argv[2]==='--write') await writeFile(target,output);
else if (process.argv.length!==2) throw new Error('Use --write to regenerate; default verifies committed vectors');
else if (await readFile(target,'utf8')!==output) throw new Error('Legacy oracle differs from committed vectors; review contract change, do not silently rewrite');
console.log(`PASS: ${vectors.length} synthetic creation vectors from legacy JS; no DB/network`);
