import {sign, timingSafeEqual, base64UrlToBytes, bytesToBase64Url} from '../auth/session.js';
import {ensureRuntimeGeneration} from '../repositories/runtime-generation.js';
import {generationBoundDb} from '../repositories/generation-bound-db.js';

const contextValue = (userName, generation) => 'application-create-context:v1:' + JSON.stringify([userName,generation]);
const failure = (message,status,code) => Object.assign(new Error(message),{status,code});
function configuration(userName, secret) {
 if (typeof userName !== 'string' || !userName || userName.length > 160) throw failure('Некорректный пользователь.',403);
 if (typeof secret !== 'string' || !secret) throw failure('Подпись запросов пока не настроена.',503);
}
async function assertGeneration(db, generation) {
 // A restore during crypto work also invalidates the context, not only before it.
 await generationBoundDb(db,generation).prepare('SELECT 1 AS creation_context_ready').first();
}

export async function issueApplicationCreationContext(db, userName, secret) {
 configuration(userName,secret);
 const generation = await ensureRuntimeGeneration(db);
 const signature = await sign(secret,contextValue(userName,generation));
 await assertGeneration(db,generation);
 // Opaque deterministic HMAC; neither user name nor raw DB generation is encoded.
 return {creationContext:'ac1.' + signature};
}
export async function verifyApplicationCreationContext(db, userName, secret, token) {
 configuration(userName,secret);
 if (token == null || token === '') throw failure('Для создания нужен контекст запроса.',428,'creation_context_required');
 if (typeof token !== 'string' || !/^ac1\.[A-Za-z0-9_-]{43}$/.test(token)) throw failure('Некорректный контекст создания.',400);
 const signature = token.slice(4);
 // Reject noncanonical base64 aliases, not just incorrect lengths/signatures.
 if (bytesToBase64Url(base64UrlToBytes(signature)) !== signature) throw failure('Некорректный контекст создания.',400);
 const generation = await ensureRuntimeGeneration(db);
 const expected = await sign(secret,contextValue(userName,generation));
 if (!timingSafeEqual(signature,expected)) throw failure('Контекст создания недействителен или устарел. Черновик сохранён; проверьте существующие отклики перед новой попыткой.',409,'creation_context_expired');
 await assertGeneration(db,generation);
 // Server-only return value for createApplicationWithReceipt; never send in JSON.
 return generation;
}
