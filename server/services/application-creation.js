import {creationUuidPattern, createApplicationWithReceipt, normalizeApplicationCreation} from '../repositories/application-creation.js';
import {readCompanyApplications} from '../repositories/applications.js';
import {generationBoundDb} from '../repositories/generation-bound-db.js';
import {verifyApplicationCreationContext} from './application-creation-context.js';

export async function createApplicationRequest(db, userName, secret, payload, options) {
 const key=payload?.idempotencyKey;
 if (!key) throw Object.assign(new Error('Для создания нужен ключ запроса.'),{status:428,code:'creation_context_required'});
 if (typeof key !== 'string' || !creationUuidPattern.test(key)) throw Object.assign(new Error('Некорректный ключ запроса.'),{status:400});
 const generation=await verifyApplicationCreationContext(db,userName,secret,payload?.creationContext);
 const input=normalizeApplicationCreation(payload);
 const guarded=generationBoundDb(db,generation);
 const result=await createApplicationWithReceipt(guarded,userName,input,{idempotencyKey:key,generation},options);
 // If this read fails after commit, a retry still recovers via the durable receipt.
 // A restore after commit also prevents returning an old result as a fresh success.
 const response=await readCompanyApplications(guarded,userName,input.companyName);
 return {...response,createdApplicationId:result.applicationId,replayed:result.replayed};
}

export async function applyToVacancyRequest(db, userName, secret, vacancyId, payload) {
 // Route is authoritative; shortcut fields retain the original fixed defaults.
 const result=await createApplicationRequest(db,userName,secret,{
  companyName:payload?.companyName, vacancyId, status:'Откликнулся', comment:'',
  applicationSources:['hh'], idempotencyKey:payload?.idempotencyKey,
  creationContext:payload?.creationContext,
 },{reuseVacancyApplication:true});
 return {ok:true,vacancy:result.vacancies.find(vacancy=>vacancy.id===vacancyId),
  createdApplicationId:result.createdApplicationId,replayed:result.replayed};
}
