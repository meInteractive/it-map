import {databaseExportRoute} from '../import-export/database-dump.js';
import {apifyRoute} from '../services/apify-route.js';
import {schedulerRoute,internalSchedulerRoute} from '../services/scheduler-route.js';
import {schedulerMcpRoute} from '../services/scheduler-mcp.js';
import {vacancyImportRoute} from '../services/vacancy-import-batches.js';
import {detailJobsRoute,enqueueDetails,startDetailJobs} from '../services/vacancy-detail-jobs.js';
import {classificationJobsRoute} from '../services/classification-jobs.js';
import {firecrawlRoute} from '../services/firecrawl-route.js';
import {classifyStoredCandidate} from '../services/stored-vacancy-classification.js';
import {upsertVacancyCandidate,getVacancyCandidates,getVacancyCandidate,linkCandidateToMainVacancy} from '../repositories/vacancy-candidates.js';
import {classifyVacancyTitle} from '../services/vacancy-classification.js';
import {readClassificationSettings,saveClassificationPrompt,readClassificationPromptInput,saveClassificationEnabled,readClassificationInput} from '../repositories/classification-prompts.js';
import {recalculateVacancyRelevance} from '../services/vacancy-relevance.js';
// Server-only; extracted from the v218 Worker.
import {automationAuthorized,readFavoriteVacancyTargets,importAutomationVacancies} from '../services/automation.js';
import {jsonResponse,htmlResponse,securityHeaders,jsonDownloadResponse,binaryDownloadResponse} from '../http.js';
import {timingSafeEqual,authenticateUser,createSession,readSession} from '../auth/session.js';
import {ensureCrmSchema} from '../repositories/schema.js';
import {validIsoDate,importVacancies,readAllVacancies,readVacancies,vacancyInput,createVacancy,saveVacancyUserFlag,deleteVacancyRecord,updateVacancy} from '../repositories/vacancies.js';
import {GOOGLE_OAUTH_STATE_COOKIE,COOKIE_NAME,SESSION_TTL_SECONDS,MAX_BACKUP_BYTES,MAX_HIRING_CONTACTS_PER_COMPANY,TASK_TYPES,CRM_STATUSES} from '../constants.js';
import {googleCalendarConfigured,validGoogleOAuthState,googleTokenRequest,googleCalendarCallbackUrl,readGoogleCalendarConnection,encryptGoogleToken,googleAccountEmail,syncUnsyncedTasks,googleCalendarStatus,createGoogleOAuthState,decryptGoogleToken} from '../integrations/google-calendar.js';
import {searchCatalogNames,readCompactEntities,readCompanyDetail,readEntityDirectory} from '../services/catalog-projection.js';
import {openaiRoute,openaiModels} from '../integrations/openai.js';
import {validateOpenaiChoice} from '../integrations/openai-models.js';
import {jevCatalogModel} from '../integrations/jev.js';
import {openRouterRoute} from '../integrations/openrouter.js';
import {ensureRuntimeGeneration} from '../repositories/runtime-generation.js';
import {generationBoundDb} from '../repositories/generation-bound-db.js';
import {readPromptModels,readPromptModelInput,savePromptModel} from '../repositories/prompt-models.js';
import {readUserResume,saveUserResume,readResumeInput} from '../repositories/resume.js';
import {readSystemPrompts,PROMPT_KINDS,saveSystemPrompt,readPromptInput} from '../repositories/prompts.js';
import {DOCUMENTATION} from '../data.js';
import {createBackup,readBackupRequest,prepareBackupRestore,restoreBackup} from '../backup/index.js';
import {companyVacancySourcesExport,companyReviewSourcesExport,importCompanies,importAgencies,importHiringContacts} from '../import-export/catalog.js';
import {companyVacancySourcesXlsx,companyReviewSourcesXlsx,companiesImportExport,companyImportExportXlsx,vacanciesExport,vacanciesXlsx} from '../import-export/xlsx.js';
import {recalculateCompanyRating,readCompanyNames,readAppCatalog} from '../repositories/catalog.js';
import {readHiringContacts,createHiringContact,deleteHiringContact,updateHiringContact} from '../repositories/contacts.js';
import {readTelegramChannels,createTelegramChannel,deleteTelegramChannel,updateTelegramChannel} from '../repositories/channels.js';
import {readCompactVacancyList,readVacancyDetail} from '../services/vacancy-projection.js';
import {streamVacancyCoverLetter,vacancyCoverLetter} from '../services/cover-letter.js';
import {issueApplicationCreationContext} from '../services/application-creation-context.js';
import {createApplicationRequest,applyToVacancyRequest} from '../services/application-creation.js';
import {readTasks,createTask,rescheduleTask,setTaskCompletion} from '../repositories/tasks.js';
import {readApplicationRecords,readCompanyApplications,normalizeRecruiterContact,normalizeApplicationSources,deleteCompanyApplication,updateCompanyApplication,readApplicationsCrm,readFavoriteCompanyNames,saveCompanyFavorite,readHiddenCompanyNames,saveCompanyHidden,readCompanyCrm,saveCompanyCrm} from '../repositories/applications.js';
import {readVacancyRoles,createVacancyRole,updateVacancyRole,deleteVacancyRole} from '../repositories/vacancy-roles.js';

async function requestHasPayload(request){
  const contentLength=request.headers.get('content-length');
  if(contentLength!==null&&Number(contentLength)>0)return true;
  const reader=request.body?.getReader();
  if(!reader)return false;
  while(true){
    const {done,value}=await reader.read();
    if(done)return false;
    if(value?.byteLength){await reader.cancel();return true;}
  }
}

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if(url.pathname==='/api/internal/database-export')return databaseExportRoute(request,env);
    if(url.pathname==='/mcp')return schedulerMcpRoute(request,env,ctx);
    if(url.pathname==='/api/internal/scheduler/tick')return internalSchedulerRoute(request,env);
    const sitePassword = env?.SITE_PASSWORD;
    const sessionSecret = env?.SESSION_SECRET;

    if (url.pathname === "/api/automation/favorites") {
      if (!automationAuthorized(request, env)) return jsonResponse({ error: "Недействительный ключ автоматизации." }, 401);
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      return jsonResponse({ targets: await readFavoriteVacancyTargets(env.DB) });
    }

    if (url.pathname === "/api/automation/vacancy-import") {
      if (!automationAuthorized(request, env)) return jsonResponse({ error: "Недействительный ключ автоматизации." }, 401);
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      let payload;
      try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
      try { return jsonResponse(await importAutomationVacancies(env.DB, payload?.rows), 201); }
      catch (error) { return jsonResponse({ error: error?.message || "Не удалось добавить вакансии." }, 400); }
    }

    const vacancyAutomationPath = url.pathname.match(/^\/api\/automation\/vacancies(?:\/(\d+))?$/);
    if (vacancyAutomationPath) {
      const expectedToken = String(env?.VACANCY_AUTOMATION_TOKEN || "");
      const authorization = String(request.headers.get("authorization") || "");
      const submittedToken = authorization.startsWith("Bearer ") ? authorization.slice(7) : "";
      if (!expectedToken || !submittedToken || !timingSafeEqual(submittedToken, expectedToken)) {
        return jsonResponse({ error: "Недействительный ключ автоматизации." }, 401);
      }
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      await ensureCrmSchema(env.DB);

      if (!vacancyAutomationPath[1] && request.method === "GET") {
        const requestedLimit = Number(url.searchParams.get("limit") || 30);
        const limit = Math.max(1, Math.min(30, Number.isSafeInteger(requestedLimit) ? requestedLimit : 30));
        const result = await env.DB.prepare(
          `SELECT id,
                  company_name AS companyName,
                  title,
                  url,
                  published_at AS publishedAt,
                  last_checked_at AS lastCheckedAt
           FROM company_vacancies
           WHERE archived = 0
             AND (last_checked_at IS NULL OR datetime(last_checked_at) < datetime('now', '-7 days'))
           ORDER BY last_checked_at IS NOT NULL ASC, datetime(last_checked_at) ASC, id ASC
           LIMIT ?`
        ).bind(limit).all();
        return jsonResponse({ vacancies: result?.results || [] });
      }

      if (vacancyAutomationPath[1] && request.method === "POST") {
        const vacancyId = Number(vacancyAutomationPath[1]);
        let payload;
        try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
        const outcome = String(payload?.outcome || "");
        if (!["active", "closed", "protected"].includes(outcome)) {
          return jsonResponse({ error: "Неизвестный результат проверки." }, 400);
        }
        const vacancy = await env.DB.prepare(
          `SELECT id,
                  published_at AS publishedAt,
                  EXISTS(SELECT 1 FROM company_applications WHERE vacancy_id = company_vacancies.id) AS hasApplication,
                  EXISTS(SELECT 1 FROM company_tasks WHERE vacancy_id = company_vacancies.id) AS hasTask
           FROM company_vacancies
           WHERE id = ?`
        ).bind(vacancyId).first();
        if (!vacancy) return jsonResponse({ error: "Вакансия не найдена." }, 404);

        if (outcome === "closed") {
          if (vacancy.hasApplication || vacancy.hasTask) {
            await env.DB.prepare(
              `UPDATE company_vacancies
               SET archived = 1, last_checked_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
               WHERE id = ?`
            ).bind(vacancyId).run();
            return jsonResponse({ ok: true, action: "archived", publicationUpdated: false });
          }
          await env.DB.prepare("DELETE FROM company_vacancies WHERE id = ?").bind(vacancyId).run();
          return jsonResponse({ ok: true, action: "deleted", publicationUpdated: false });
        }

        let publishedAt = null;
        if (outcome === "active" && payload?.publishedAt != null && payload.publishedAt !== "") {
          publishedAt = String(payload.publishedAt);
          if (!validIsoDate(publishedAt)) return jsonResponse({ error: "Некорректная дата публикации." }, 400);
        }
        const publicationUpdated = Boolean(publishedAt && publishedAt !== vacancy.publishedAt);
        await env.DB.prepare(
          `UPDATE company_vacancies
           SET published_at = CASE WHEN ? = 1 THEN ? ELSE published_at END,
               last_checked_at = CURRENT_TIMESTAMP,
               updated_at = CASE WHEN ? = 1 THEN CURRENT_TIMESTAMP ELSE updated_at END
           WHERE id = ?`
        ).bind(publicationUpdated ? 1 : 0, publishedAt, publicationUpdated ? 1 : 0, vacancyId).run();
        return jsonResponse({ ok: true, action: outcome === "protected" ? "protected" : "checked", publicationUpdated });
      }

      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }


    if (url.pathname === "/api/google-calendar/callback") {
      const clearStateCookie = `${GOOGLE_OAUTH_STATE_COOKIE}=; Path=/api/google-calendar/callback; Max-Age=0; HttpOnly; Secure; SameSite=Lax`;
      if (!env?.DB || !googleCalendarConfigured(env)) return htmlResponse("Интеграция Google Calendar пока не настроена.", 503, { "set-cookie": clearStateCookie });
      const state = String(url.searchParams.get("state") || "");
      const googleUserName = await validGoogleOAuthState(request, sessionSecret, state);
      if (!googleUserName) return htmlResponse("Не удалось подтвердить безопасное подключение Google Calendar.", 400, { "set-cookie": clearStateCookie });
      const code = String(url.searchParams.get("code") || "");
      if (!code || url.searchParams.get("error")) {
        return new Response(null, { status: 303, headers: { ...securityHeaders, location: "/?calendar=cancelled", "set-cookie": clearStateCookie } });
      }
      try {
        const token = await googleTokenRequest(env, {
          client_id: env.GOOGLE_CALENDAR_CLIENT_ID,
          client_secret: env.GOOGLE_CALENDAR_CLIENT_SECRET,
          code,
          redirect_uri: googleCalendarCallbackUrl(url),
          grant_type: "authorization_code"
        });
        const current = await readGoogleCalendarConnection(env.DB, googleUserName);
        const encryptedRefreshToken = token.refresh_token
          ? await encryptGoogleToken(sessionSecret, token.refresh_token)
          : current?.refreshTokenEncrypted;
        if (!encryptedRefreshToken) throw new Error("Google не выдал разрешение для фоновой синхронизации.");
        const accountEmail = await googleAccountEmail(token.access_token);
        await env.DB.prepare(
          `INSERT INTO google_calendar_user_connection
             (user_name, account_email, refresh_token_encrypted, calendar_id, connected_at, updated_at)
           VALUES (?, ?, ?, 'primary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
           ON CONFLICT(user_name) DO UPDATE SET
             account_email = excluded.account_email,
             refresh_token_encrypted = excluded.refresh_token_encrypted,
             calendar_id = excluded.calendar_id,
             connected_at = CURRENT_TIMESTAMP,
             updated_at = CURRENT_TIMESTAMP`
        ).bind(googleUserName, accountEmail, encryptedRefreshToken).run();
        await syncUnsyncedTasks(env, googleUserName, url.origin).catch(() => ({ synced: 0, errors: 1 }));
        return new Response(null, { status: 303, headers: { ...securityHeaders, location: "/?calendar=connected", "set-cookie": clearStateCookie } });
      } catch {
        return new Response(null, { status: 303, headers: { ...securityHeaders, location: "/?calendar=error", "set-cookie": clearStateCookie } });
      }
    }

    if (!sitePassword || !sessionSecret) {
      return htmlResponse("Доступ временно не настроен.", 503);
    }

    if (url.pathname === "/auth" && request.method === "POST") {
      const form = await request.formData();
      const submittedLogin = String(form.get("login") || "");
      const submittedPassword = String(form.get("password") || "");
      const authenticatedUser = await authenticateUser(submittedLogin, submittedPassword, env, sessionSecret);
      if (authenticatedUser) {
        const session = await createSession(sessionSecret, authenticatedUser.name);
        return new Response(null, {
          status: 303,
          headers: {
            ...securityHeaders,
            location: "/",
            "set-cookie": `${COOKIE_NAME}=${session}; Path=/; Max-Age=${SESSION_TTL_SECONDS}; HttpOnly; Secure; SameSite=Strict`
          }
        });
      }
      return new Response(null,{status:401,headers:{...securityHeaders,'x-app-login-error':'1'}});
    }

    if (url.pathname === "/logout") {
      return new Response(null, {
        status: 303,
        headers: {
          ...securityHeaders,
          location: "/",
          "set-cookie": `${COOKIE_NAME}=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Strict`
        }
      });
    }

    const currentUser = await readSession(request, sessionSecret);
    if (!currentUser) {
      if (url.pathname.startsWith("/api/")) {
        return jsonResponse({ error: "Требуется войти в систему." }, 401);
      }
      return null;
    }

    if(url.pathname==='/api/session'&&request.method==='GET')return jsonResponse(currentUser);
    const adminOnlyPaths = new Set([
      "/api/backup", "/api/backup/validate", "/api/exports/companies-vacancies", "/api/exports/companies-reviews",
      "/api/exports/companies", "/api/exports/vacancies", "/api/companies/import",
      "/api/companies/recalculate-rating", "/api/vacancies/import"
    ]);
    if (!currentUser.isAdmin && adminOnlyPaths.has(url.pathname)) {
      return jsonResponse({ error: "Доступно только администратору." }, 403);
    }

    const catalogRoute=url.pathname.match(/^\/api\/(companies|agencies)\/(list|map|search|detail|[0-9]+)$/);
    if(catalogRoute){
      if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(!env?.DB)return jsonResponse({error:'Хранилище компаний не подключено.'},503);
      const type=catalogRoute[1]==='agencies'?'agency':'company',mode=catalogRoute[2];
      if(mode==='search')return jsonResponse({matches:await searchCatalogNames(env.DB,String(url.searchParams.get('q')||'').slice(0,256))});
      if(mode==='list'||mode==='map')return jsonResponse({companies:await readCompactEntities(env.DB,currentUser.name,mode,type)});
      const company=await readCompanyDetail(env.DB,currentUser.name,mode==='detail'?null:Number(mode),url.searchParams.get('name'),type);
      return company?jsonResponse({company}):jsonResponse({error:'Компания не найдена.'},404);
    }

    if(url.pathname==='/api/openai'||url.pathname.startsWith('/api/openai/')){
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      return openaiRoute(request,env,url);
    }

    if (url.pathname === "/api/openrouter" || url.pathname.startsWith("/api/openrouter/")) {
      if (!currentUser.isAdmin) return jsonResponse({error:"Доступно только администратору."},403);
      if(url.pathname==='/api/openrouter/jev'){
        if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
        if(!env?.DB)return jsonResponse({error:'Хранилище не подключено.'},503);
        try{return jsonResponse(await jevCatalogModel(env,request.signal));}
        catch(error){return jsonResponse({error:error.message||'Не удалось получить тариф JEV.',code:error.code||'internal'},error.status||500);}
      }
      return openRouterRoute(request,env,url);
    }

    if (url.pathname === "/api/profile/resume") {
      if (!["GET","PUT"].includes(request.method)) return jsonResponse({error:"Метод не поддерживается."},405);
      if (!env?.DB) return jsonResponse({error:"Хранилище резюме пока не подключено."},503);
      try {
        if (request.method === "GET") return jsonResponse(await readUserResume(env.DB,currentUser.name));
        const origin=request.headers.get("origin");
        if(origin && origin !== url.origin) return jsonResponse({error:"Недопустимый источник запроса."},403);
        return jsonResponse(await saveUserResume(env.DB,currentUser.name,await readResumeInput(request)));
      } catch(error) {return jsonResponse({error:error?.message || "Не удалось сохранить резюме."},error?.status || 400);}
    }

    if(url.pathname==='/api/profile/prompt-models'||url.pathname.startsWith('/api/profile/prompt-models/')){
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      if(!env?.DB)return jsonResponse({error:'Хранилище не подключено.'},503);
      if(url.pathname==='/api/profile/prompt-models'&&request.method==='GET')return jsonResponse({models:await readPromptModels(env.DB)});
      const kind=url.pathname.slice('/api/profile/prompt-models/'.length);
      if(!PROMPT_KINDS.includes(kind))return jsonResponse({error:'Промт не найден.'},404);
      if(request.method!=='PUT')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      try{
        const guardedDb=generationBoundDb(env.DB,await ensureRuntimeGeneration(env.DB));
        const input=await readPromptModelInput(request);
        if(input.openrouterModel==='jev'&&!['relevance','vacancy_role'].includes(kind))return jsonResponse({error:'JEV доступна для релевантности и определения конечных ролей.'},400);
        if(input.provider==='openrouter'&&input.openrouterModel==='jev')await jevCatalogModel({...env,DB:guardedDb});
        if(input.provider==='openai'){
          validateOpenaiChoice(input.openaiModel,input.reasoningEffort);
          const available=await openaiModels(env);
          if(input.openaiModel&&!available.some(m=>m.id===input.openaiModel))return jsonResponse({error:'Выбранная модель отсутствует в списке доступных OpenAI API-моделей.'},400);
          if(!available.length)return jsonResponse({error:'Нет доступных текстовых моделей OpenAI.'},409);
        }
        return jsonResponse(await savePromptModel(guardedDb,kind,input));
      }
      catch(error){return jsonResponse({error:error.message||'Не удалось сохранить настройки.',code:error.code},error.status||400);}
    }

    if (url.pathname === "/api/profile/prompts" || url.pathname.startsWith("/api/profile/prompts/")) {
      if (!currentUser.isAdmin) return jsonResponse({error:"Доступно только администратору."},403);
      if (!env?.DB) return jsonResponse({error:"Хранилище промтов пока не подключено."},503);
      if (url.pathname === "/api/profile/prompts" && request.method === "GET") return jsonResponse(await readSystemPrompts(env.DB));
      const kind=url.pathname.slice('/api/profile/prompts/'.length);
      if (url.pathname === "/api/profile/prompts" || !PROMPT_KINDS.includes(kind)) return jsonResponse({error:"Промт не найден."},404);
      if (request.method !== "PUT") return jsonResponse({error:"Метод не поддерживается."},405);
      const origin=request.headers.get("origin");
      if(origin && origin!==url.origin)return jsonResponse({error:"Недопустимый источник запроса."},403);
      try{return jsonResponse(await saveSystemPrompt(env.DB,kind,await readPromptInput(request,kind)));}
      catch(error){return jsonResponse({error:error?.message||"Не удалось сохранить промт."},error?.status||400);}
    }

    if(url.pathname==='/api/scheduler'||url.pathname.startsWith('/api/scheduler/'))return schedulerRoute(request,env,url,currentUser,ctx);
    if(url.pathname==='/api/vacancy-import'||url.pathname.startsWith('/api/vacancy-import/'))return vacancyImportRoute(request,env,url,currentUser,ctx);
    if(url.pathname==='/api/vacancy-details'||url.pathname.startsWith('/api/vacancy-details/'))return detailJobsRoute(request,env,url,currentUser,ctx);
    if(url.pathname.startsWith('/api/classification/jobs'))return classificationJobsRoute(request,env,url,currentUser,ctx);
    if(url.pathname.startsWith('/api/apify/'))return apifyRoute(request,env,url,currentUser);
    if(url.pathname.startsWith('/api/firecrawl/'))return firecrawlRoute(request,env,url,currentUser,ctx);

    if(url.pathname==='/api/vacancy-candidates'||url.pathname.startsWith('/api/vacancy-candidates/')){
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      if(!env?.DB)return jsonResponse({error:'Хранилище пока не подключено.'},503);
      if(request.method!=='GET'&&request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      try{
        if(url.pathname==='/api/vacancy-candidates'){
          if(request.method==='GET'){
            const allowed=['companyId','agencyId','sourceId','sourceNamespace','status','page','pageSize','order'];
            if([...url.searchParams.keys()].some(key=>!allowed.includes(key)))throw Object.assign(new Error('Неизвестный фильтр.'),{status:400});
            return jsonResponse(await getVacancyCandidates(env.DB,Object.fromEntries(url.searchParams)));
          }
          if(request.method==='POST'){const result=await upsertVacancyCandidate(env.DB,await readClassificationInput(request));return jsonResponse(result,result.created?201:200);}
          return jsonResponse({error:'Метод не поддерживается.'},405);
        }
        const match=url.pathname.match(/^\/api\/vacancy-candidates\/(\d+)(?:\/(link|classify))?$/);
        if(!match)return jsonResponse({error:'Метод не найден.'},404);
        if(!match[2]){
          if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
          if([...url.searchParams.keys()].some(key=>!['historyPage','historyPageSize'].includes(key)))throw Object.assign(new Error('Неизвестный параметр истории.'),{status:400});
          const result=await getVacancyCandidate(env.DB,Number(match[1]),Object.fromEntries(url.searchParams));
          return result?jsonResponse(result):jsonResponse({error:'Первичная вакансия не найдена.'},404);
        }
        if(match[2]==='classify'){
          if(request.method!=='POST')return jsonResponse({error:'Метод не поддерживается.'},405);
          const input=await readClassificationInput(request);if(Object.keys(input).some(key=>key!=='force')||(input.force!==undefined&&typeof input.force!=='boolean'))throw new Error('Передайте force: true или false.');
          const result=await classifyStoredCandidate(env,Number(match[1]),{force:input.force??false,signal:request.signal});if(result.candidate.processingStatus==='accepted'){const next=await enqueueDetails(env,{candidateId:Number(match[1])},'classification:'+String(result.attemptId||match[1])).catch(()=>({jobIds:[]}));startDetailJobs(env,ctx,next.jobIds);}return jsonResponse(result);
        }
        if(request.method!=='PUT')return jsonResponse({error:'Метод не поддерживается.'},405);
        const input=await readClassificationInput(request);if(Object.keys(input).some(key=>key!=='mainVacancyId'))throw new Error('Передайте mainVacancyId.');
        return jsonResponse(await linkCandidateToMainVacancy(env.DB,Number(match[1]),input.mainVacancyId));
      }catch(error){return jsonResponse({error:error.status||error.code==='restored'?error.message:'Не удалось обработать данные первичной вакансии.',code:error.code},error.status||400);}
    }

    if(url.pathname==='/api/classification/test'){
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      if(!env?.DB)return jsonResponse({error:'Хранилище пока не подключено.'},503);
      if(request.method!=='POST')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      try{
        const input=await readClassificationInput(request);
        if(Object.keys(input).some(key=>key!=='title'))throw new Error('Передайте только title.');
        return jsonResponse(await classifyVacancyTitle(env,input.title,{signal:request.signal}));
      }catch(error){return jsonResponse({error:error.code?error.message:'Не удалось проверить название вакансии: '+(error.status===400?error.message:'проверьте данные и повторите запрос.'),code:error.code},error.status||400);}
    }

    if(url.pathname==='/api/classification/settings'||url.pathname.startsWith('/api/classification/prompts/')||url.pathname.startsWith('/api/classification/roles/')){
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      if(!env?.DB)return jsonResponse({error:'Хранилище пока не подключено.'},503);
      if(url.pathname==='/api/classification/settings'){
        if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
        return jsonResponse(await readClassificationSettings(env.DB));
      }
      const match=url.pathname.match(/^\/api\/classification\/(prompts|roles)\/(\d+)$/);
      if(!match)return jsonResponse({error:'Промт не найден.'},404);
      if(request.method!=='PUT')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(request.headers.get('origin')&&request.headers.get('origin')!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      try{return jsonResponse(match[1]==='prompts'?await saveClassificationPrompt(env.DB,Number(match[2]),await readClassificationPromptInput(request)):await saveClassificationEnabled(env.DB,Number(match[2]),await readClassificationInput(request)));}
      catch(error){return jsonResponse({error:error.message||'Не удалось сохранить промт.'},error.status||400);}
    }

    if (url.pathname === "/api/docs" || url.pathname.startsWith("/api/docs/")) {
      if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      if (url.pathname === "/api/docs") return jsonResponse({ revision: DOCUMENTATION.revision, updatedAt: DOCUMENTATION.updatedAt, versions: DOCUMENTATION.versions, sections: DOCUMENTATION.sections.map(({ markdown, ...meta }) => meta) });
      const id = url.pathname.slice("/api/docs/".length);
      const section = DOCUMENTATION.sections.find(item => item.id === id);
      if (!section) return jsonResponse({ error: "Раздел документации не найден." }, 404);
      return new Response(section.markdown, { headers: { ...securityHeaders, "content-type": "text/markdown; charset=utf-8", "cache-control": "private, no-store", "content-disposition": `${url.searchParams.get("download") === "1" ? "attachment" : "inline"}; filename="${section.file}"` } });
    }

    if (url.pathname === "/api/google-calendar/status") {
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      return jsonResponse(await googleCalendarStatus(env, currentUser.name));
    }

    if (url.pathname === "/api/google-calendar/connect") {
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      if (!env?.DB || !googleCalendarConfigured(env)) return jsonResponse({ error: "Сначала добавьте Google OAuth Client ID и Client Secret в настройки сайта." }, 503);
      const state = await createGoogleOAuthState(sessionSecret, currentUser.name);
      const authorizationUrl = new URL("https://accounts.google.com/o/oauth2/v2/auth");
      authorizationUrl.search = new URLSearchParams({
        client_id: env.GOOGLE_CALENDAR_CLIENT_ID,
        redirect_uri: googleCalendarCallbackUrl(url),
        response_type: "code",
        access_type: "offline",
        prompt: "consent",
        include_granted_scopes: "true",
        scope: "openid email https://www.googleapis.com/auth/calendar.events",
        state
      }).toString();
      return new Response(null, {
        status: 303,
        headers: {
          ...securityHeaders,
          location: authorizationUrl.toString(),
          "set-cookie": `${GOOGLE_OAUTH_STATE_COOKIE}=${state}; Path=/api/google-calendar/callback; Max-Age=600; HttpOnly; Secure; SameSite=Lax`
        }
      });
    }

    if (url.pathname === "/api/google-calendar/sync") {
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      if (!env?.DB || !googleCalendarConfigured(env)) return jsonResponse({ error: "Интеграция Google Calendar не настроена." }, 503);
      try {
        const connection = await readGoogleCalendarConnection(env.DB, currentUser.name);
        if (!connection) return jsonResponse({ error: "Сначала подключите Google Calendar." }, 409);
        return jsonResponse(await syncUnsyncedTasks(env, currentUser.name, url.origin));
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось синхронизировать задачи." }, 400);
      }
    }

    if (url.pathname === "/api/google-calendar/disconnect") {
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      if (!env?.DB) return jsonResponse({ error: "Хранилище интеграции пока не подключено." }, 503);
      const connection = await readGoogleCalendarConnection(env.DB, currentUser.name);
      if (connection && googleCalendarConfigured(env)) {
        try {
          const refreshToken = await decryptGoogleToken(sessionSecret, connection.refreshTokenEncrypted);
          await fetch("https://oauth2.googleapis.com/revoke", {
            method: "POST",
            headers: { "content-type": "application/x-www-form-urlencoded" },
            body: new URLSearchParams({ token: refreshToken })
          });
        } catch {}
      }
      await env.DB.batch([
        env.DB.prepare("DELETE FROM google_calendar_user_connection WHERE user_name = ?").bind(currentUser.name),
        env.DB.prepare("UPDATE company_tasks SET google_event_id = NULL, google_event_html_link = NULL, google_sync_status = '', google_sync_error = '' WHERE user_name = ?").bind(currentUser.name)
      ]);
      return jsonResponse({ ok: true });
    }

    if (url.pathname === "/api/backup" || url.pathname === "/api/backup/validate") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище данных пока не подключено." }, 503);
      if (request.method === "GET" && url.pathname === "/api/backup") {
        try {
          const backup = await createBackup(env.DB);
          return jsonDownloadResponse(backup, `it-employers-backup-${backup.createdAt.slice(0, 10)}.json`);
        } catch (error) { return jsonResponse({error:error?.message || "Не удалось создать полный бэкап."}, 400); }
      }
      if (request.method === "POST") {
        const origin = request.headers.get("origin");
        if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
        try {
          const payload = await readBackupRequest(request);
          if (url.pathname === "/api/backup/validate") {
            const plan = await prepareBackupRestore(env.DB, payload);
            return jsonResponse({ok:true, tables:plan.tables.length, rows:plan.tables.reduce((sum, table) => sum + table.rows.length, 0), warnings:plan.warnings});
          }
          return jsonResponse(await restoreBackup(env.DB, payload));
        } catch (error) {
          return jsonResponse({ error: error?.message || "Не удалось восстановить данные." }, error?.status || 400);
        }
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    if (url.pathname === "/api/exports/companies-vacancies") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище данных пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const onlyUnranked = url.searchParams.get("unranked") === "1";
      const rows = await companyVacancySourcesExport(env.DB, currentUser.name, onlyUnranked);
      const date = new Date().toISOString().slice(0, 10);
      return binaryDownloadResponse(
        companyVacancySourcesXlsx(rows),
        `it-employers-vacancy-sources${onlyUnranked ? "-unranked" : ""}-${date}.xlsx`
      );
    }

    if (url.pathname === "/api/exports/companies-reviews") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище данных пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const rows = await companyReviewSourcesExport(env.DB, currentUser.name);
      const date = new Date().toISOString().slice(0, 10);
      return binaryDownloadResponse(
        companyReviewSourcesXlsx(rows),
        `it-employers-review-sources-${date}.xlsx`
      );
    }

    if (url.pathname === "/api/exports/companies") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище компаний пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const data = await companiesImportExport(env.DB);
      const date = new Date().toISOString().slice(0, 10);
      return binaryDownloadResponse(
        companyImportExportXlsx(data),
        `it-employers-companies-${date}.xlsx`
      );
    }

    if (url.pathname === "/api/exports/vacancies") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const rows = await vacanciesExport(env.DB, currentUser.name);
      const date = new Date().toISOString().slice(0, 10);
      return binaryDownloadResponse(
        vacanciesXlsx(rows),
        `it-employers-vacancies-${date}.xlsx`
      );
    }

    if (url.pathname === "/api/companies/import") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище компаний пока не подключено." }, 503);
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      const contentLength = Number(request.headers.get("content-length") || "0");
      if (contentLength > MAX_BACKUP_BYTES) return jsonResponse({ error: "Данные импорта больше допустимых 25 МБ." }, 413);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Не удалось прочитать данные из Excel-файла." }, 400);
      }
      try {
        const companyResult = await importCompanies(env.DB, payload?.companies, payload?.offices, payload?.reviews, payload?.mode);
        const agencyResult = await importAgencies(env.DB, payload?.agencies, payload?.mode);
        const contactResult = await importHiringContacts(env.DB, payload?.contacts, payload?.mode);
        return jsonResponse({ ...companyResult, ...agencyResult, ...contactResult, errors: [...(companyResult.errors || []), ...(agencyResult.agencyErrors || []), ...(contactResult.contactErrors || [])] });
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось импортировать компании." }, 400);
      }
    }

    if (url.pathname === "/api/companies/recalculate-rating") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище компаний пока не подключено." }, 503);
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      try {
        return jsonResponse(await recalculateCompanyRating(env.DB));
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось пересчитать рейтинг." }, 400);
      }
    }

    if (url.pathname === "/api/vacancies/import") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      const contentLength = Number(request.headers.get("content-length") || "0");
      if (contentLength > MAX_BACKUP_BYTES) return jsonResponse({ error: "Данные импорта больше допустимых 25 МБ." }, 413);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Не удалось прочитать данные из Excel-файла." }, 400);
      }
      try {
        return jsonResponse(await importVacancies(env.DB, currentUser.name, payload?.rows, payload?.mode));
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось импортировать вакансии." }, 400);
      }
    }

    if (url.pathname === "/api/hiring-contacts") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище контактов пока не подключено." }, 503);
      const companyName = String(url.searchParams.get("company") || "").trim();
      if (request.method === "GET") {
        const companyNames = await readCompanyNames(env.DB);
        if (!companyName || !companyNames.has(companyName)) return jsonResponse({ error: "Компания или агентство не найдены." }, 404);
        return jsonResponse({ companyName, limit: MAX_HIRING_CONTACTS_PER_COMPANY, contacts: await readHiringContacts(env.DB, companyName) });
      }
      if (request.method === "POST") {
        if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
        const origin = request.headers.get("origin");
        if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
        let payload;
        try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
        const ownerName = String(payload?.companyName || "").trim(), companyNames = await readCompanyNames(env.DB);
        if (!ownerName || !companyNames.has(ownerName)) return jsonResponse({ error: "Компания или агентство не найдены." }, 404);
        try { return jsonResponse(await createHiringContact(env.DB, ownerName, payload), 201); }
        catch (error) { return jsonResponse({ error: error?.message || "Не удалось добавить контакт." }, 400); }
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    const hiringContactPathMatch = url.pathname.match(/^\/api\/hiring-contacts\/(\d+)$/);
    if (hiringContactPathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище контактов пока не подключено." }, 503);
      if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      const contactId = Number(hiringContactPathMatch[1]);
      if (request.method === "DELETE") {
        const result = await deleteHiringContact(env.DB, contactId);
        return result ? jsonResponse(result) : jsonResponse({ error: "Контакт не найден." }, 404);
      }
      if (request.method === "PUT") {
        let payload;
        try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
        try {
          const result = await updateHiringContact(env.DB, contactId, payload);
          return result ? jsonResponse(result) : jsonResponse({ error: "Контакт не найден." }, 404);
        } catch (error) { return jsonResponse({ error: error?.message || "Не удалось обновить контакт." }, 400); }
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    if (url.pathname === "/api/channels") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище каналов пока не подключено." }, 503);
      if (request.method === "GET") return jsonResponse({ channels: await readTelegramChannels(env.DB) });
      if (request.method !== "POST") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      let payload;
      try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
      try { return jsonResponse(await createTelegramChannel(env.DB, payload), 201); }
      catch (error) { return jsonResponse({ error: error?.message || "Не удалось добавить канал." }, 400); }
    }

    const channelPathMatch = url.pathname.match(/^\/api\/channels\/(\d+)$/);
    if (channelPathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище каналов пока не подключено." }, 503);
      if (!["PUT", "DELETE"].includes(request.method)) return jsonResponse({ error: "Метод не поддерживается." }, 405);
      if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      const channelId = Number(channelPathMatch[1]);
      if (request.method === "DELETE") {
        try {
          const deleted = await deleteTelegramChannel(env.DB, channelId);
          return deleted ? jsonResponse(deleted) : jsonResponse({ error: "Канал не найден." }, 404);
        } catch (error) {
          return jsonResponse({ error: error?.message || "Не удалось удалить канал." }, error?.status || 400);
        }
      }
      let payload;
      try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
      try {
        const updated = await updateTelegramChannel(env.DB, channelId, payload);
        return updated ? jsonResponse(updated) : jsonResponse({ error: "Канал не найден." }, 404);
      } catch (error) { return jsonResponse({ error: error?.message || "Не удалось обновить канал." }, 400); }
    }

    if (url.pathname === '/api/vacancy-roles') {
      if (!env?.DB) return jsonResponse({error:'Хранилище ролей пока не подключено.'},503);
      if (request.method === 'GET') { await ensureCrmSchema(env.DB); return jsonResponse(await readVacancyRoles(env.DB)); }
      if (request.method !== 'POST') return jsonResponse({error:'Метод не поддерживается.'},405);
      if (!currentUser.isAdmin) return jsonResponse({error:'Доступно только администратору.'},403);
      const origin=request.headers.get('origin');
      if(origin&&origin!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      let payload;try{payload=await request.json();}catch{return jsonResponse({error:'Некорректные данные.'},400);}
      try{return jsonResponse(await createVacancyRole(env.DB,payload),201);}
      catch(error){return jsonResponse({error:error?.message||'Не удалось добавить роль.'},400);}
    }

    if (url.pathname === '/api/vacancy-roles/roots' || url.pathname === '/api/vacancy-roles/leaves') {
      if (request.method !== 'GET') return jsonResponse({error:'Метод не поддерживается.'},405);
      if (!env?.DB) return jsonResponse({error:'Хранилище ролей пока не подключено.'},503);
      await ensureCrmSchema(env.DB);
      const data=await readVacancyRoles(env.DB);
      return jsonResponse({roles:url.pathname.endsWith('/roots')?data.roots:data.leaves});
    }

    const vacancyRoleDescendants=url.pathname.match(/^\/api\/vacancy-roles\/(\d+)\/descendants$/);
    if(vacancyRoleDescendants){
      if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(!env?.DB)return jsonResponse({error:'Хранилище ролей пока не подключено.'},503);
      await ensureCrmSchema(env.DB);
      const data=await readVacancyRoles(env.DB),role=data.roles.find(item=>item.id===Number(vacancyRoleDescendants[1]));
      return role?jsonResponse({role,roles:data.roles.filter(item=>role.descendantIds.includes(item.id))}):jsonResponse({error:'Роль не найдена.'},404);
    }

    const vacancyRolePath=url.pathname.match(/^\/api\/vacancy-roles\/(\d+)$/);
    if(vacancyRolePath){
      if(!env?.DB)return jsonResponse({error:'Хранилище ролей пока не подключено.'},503);
      if(!['PUT','DELETE'].includes(request.method))return jsonResponse({error:'Метод не поддерживается.'},405);
      if(!currentUser.isAdmin)return jsonResponse({error:'Доступно только администратору.'},403);
      const origin=request.headers.get('origin');
      if(origin&&origin!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      try{
        const result=request.method==='DELETE'
          ? await deleteVacancyRole(env.DB,Number(vacancyRolePath[1]))
          : await updateVacancyRole(env.DB,Number(vacancyRolePath[1]),await request.json());
        return result?jsonResponse(result):jsonResponse({error:'Роль не найдена.'},404);
      }catch(error){return jsonResponse({error:error?.message||'Не удалось изменить роль.'},400);}
    }

    if(url.pathname==='/api/vacancies/list'){
      if(request.method!=='GET')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(!env?.DB)return jsonResponse({error:'Хранилище вакансий пока не подключено.'},503);
      const period=url.searchParams.get('period');
      if(period!==null&&!['week','month','twoMonths','archive','all'].includes(period))return jsonResponse({error:'Некорректный период публикации.'},400);
      return jsonResponse(await readCompactVacancyList(env.DB,currentUser.name,{period:period||undefined}));
    }

    if (url.pathname === "/api/vacancies") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method === "GET") {
        const companyName = String(url.searchParams.get("company") || "").trim();
        const channelId = Number(url.searchParams.get("channel") || 0);
        if (Number.isSafeInteger(channelId) && channelId > 0) {
          const vacancies = (await readAllVacancies(env.DB, currentUser.name)).filter((vacancy) => vacancy.telegramSources.some((source) => source.channelId === channelId));
          return jsonResponse({channelId,vacancies,entities:await readEntityDirectory(env.DB,vacancies.flatMap(v=>[v.companyName,v.realCompanyName]),true)});
        }
        if (!companyName) {
          return jsonResponse({
            vacancies: await readAllVacancies(env.DB, currentUser.name),
            entities: await readCompactEntities(env.DB,currentUser.name,'index','all'),
            applicationSummary: (await env.DB.prepare('SELECT company_name AS companyName,vacancy_id AS vacancyId FROM company_applications WHERE user_name=?').bind(currentUser.name).all()).results||[]
          });
        }
        const companyNames = await readCompanyNames(env.DB);
        if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
        const vacancies=await readVacancies(env.DB,currentUser.name,companyName);
        return jsonResponse({companyName,limit:null,vacancies,
          entities:await readEntityDirectory(env.DB,[companyName,...vacancies.flatMap(v=>[v.companyName,v.realCompanyName])],true)
        });
      }
      if (request.method === "POST") {
        if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
        const origin = request.headers.get("origin");
        if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
        let payload;
        try {
          payload = await request.json();
        } catch {
          return jsonResponse({ error: "Некорректные данные." }, 400);
        }
        try {
          if (payload?.coverLetterScope === "personal") throw new Error("Личное письмо нельзя копировать в общие поля новой вакансии.");
          const input = vacancyInput(payload);
          const catalog = await readAppCatalog(env.DB);
          if (input.entityType === "channel") {
            const channel = await env.DB.prepare("SELECT id FROM telegram_channels WHERE id = ?").bind(input.telegramChannelId).first();
            if (!channel) return jsonResponse({ error: "Telegram-канал не найден." }, 404);
          } else {
            const owners = input.entityType === "agency" ? new Set(catalog.agencies.map((agency) => agency.name)) : new Set(catalog.companies.map((company) => company.name));
            if (!owners.has(input.companyName)) return jsonResponse({ error: input.entityType === "agency" ? "Агентство не найдено." : "Компания не найдена." }, 404);
          }
          if (input.entityType === "agency" && input.realCompanyName && !catalog.companies.some((company) => company.name === input.realCompanyName)) return jsonResponse({ error: "Реальный работодатель не найден." }, 404);
          return jsonResponse(await createVacancy(env.DB, currentUser.name, input), 201);
        } catch (error) {
          return jsonResponse({ error: error?.message || "Не удалось сохранить вакансию." }, 400);
        }
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    const vacancyRelevanceMatch=url.pathname.match(/^\/api\/vacancies\/(\d+)\/relevance$/);
    if(vacancyRelevanceMatch){
      if(request.method!=='POST')return jsonResponse({error:'Метод не поддерживается.'},405);
      if(!env?.DB)return jsonResponse({error:'Хранилище вакансий пока не подключено.'},503);
      const origin=request.headers.get('origin');
      if(origin&&origin!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      if(await requestHasPayload(request))return jsonResponse({error:'Расчёт использует сохранённые данные вакансии; тело запроса не требуется.'},400);
      const id=Number(vacancyRelevanceMatch[1]);
      if(!Number.isSafeInteger(id)||id<1)return jsonResponse({error:'Некорректный ID вакансии.'},400);
      try{const result=await recalculateVacancyRelevance(env.DB,env,currentUser.name,id,request.signal);
        return result?jsonResponse(result):jsonResponse({error:'Вакансия не найдена.'},404);
      }catch(error){return jsonResponse({error:error?.message||'Не удалось рассчитать релевантность.',code:error?.code},error?.status||502);}
    }

    const vacancyLetterPathMatch = url.pathname.match(/^\/api\/vacancies\/(\d+)\/cover-letter(\/regenerate)?$/);
    if (vacancyLetterPathMatch) {
      if (request.method !== 'POST') return jsonResponse({error:'Метод не поддерживается.'},405);
      if (!env?.DB) return jsonResponse({error:'Хранилище вакансий пока не подключено.'},503);
      const origin=request.headers.get('origin');
      if(origin && origin!==url.origin)return jsonResponse({error:'Недопустимый источник запроса.'},403);
      const letterOptions={regenerate:!!vacancyLetterPathMatch[2]};
      if(request.headers.get('accept')?.includes('application/x-ndjson'))return streamVacancyCoverLetter(env.DB,env,currentUser.name,Number(vacancyLetterPathMatch[1]),letterOptions);
      try {
        const letter=await vacancyCoverLetter(env.DB,env,currentUser.name,Number(vacancyLetterPathMatch[1]),undefined,letterOptions);
        return letter?jsonResponse(letter):jsonResponse({error:'Вакансия не найдена.'},404);
      } catch(error) {return jsonResponse({error:error?.message||'Не удалось создать письмо.'},error?.status||502);}
    }

    const vacancyStatePathMatch = url.pathname.match(/^\/api\/vacancies\/(\d+)\/state$/);
    if (vacancyStatePathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if (request.method !== "PUT") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      let payload;
      try { payload = await request.json(); } catch { return jsonResponse({ error: "Некорректные данные." }, 400); }
      const keys = ["favorite", "hidden"].filter((key) => typeof payload?.[key] === "boolean");
      if (keys.length !== 1) return jsonResponse({ error: "Укажите один изменяемый признак вакансии." }, 400);
      try {
        const state = await saveVacancyUserFlag(env.DB, currentUser.name, Number(vacancyStatePathMatch[1]), keys[0], payload[keys[0]]);
        if (!state) return jsonResponse({ error: "Вакансия не найдена." }, 404);
        return jsonResponse({ ok: true, ...state });
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось изменить признаки вакансии." }, 400);
      }
    }

    const protectedVacancyApplyMatch = url.pathname.match(/^\/api\/vacancies\/(\d+)\/apply(?:\/create)?$/);
    if (protectedVacancyApplyMatch) {
      if (!env?.DB) return jsonResponse({error:'Хранилище вакансий пока не подключено.'},503);
      if (request.method !== 'POST') return jsonResponse({error:'Метод не поддерживается.'},405);
      const origin=request.headers.get('origin');
      if (origin && origin !== url.origin) return jsonResponse({error:'Недопустимый источник запроса.'},403);
      let payload;
      try { const text=await request.text(); payload=text.trim()?JSON.parse(text):{}; }
      catch { return jsonResponse({error:'Некорректные данные.'},400); }
      try {
        return jsonResponse(await applyToVacancyRequest(env.DB,currentUser.name,sessionSecret,Number(protectedVacancyApplyMatch[1]),payload));
      } catch (error) {
        return jsonResponse({error:error?.message||'Не удалось создать отклик.',code:error?.code},error?.status||503);
      }
    }

    const vacancyPathMatch = url.pathname.match(/^\/api\/vacancies\/(\d+)$/);
    if (vacancyPathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище вакансий пока не подключено." }, 503);
      if(request.method==='GET'){
        const detail=await readVacancyDetail(env.DB,currentUser.name,Number(vacancyPathMatch[1]));
        return detail?jsonResponse(detail):jsonResponse({error:'Вакансия не найдена.'},404);
      }
      if (!["PUT", "DELETE"].includes(request.method)) return jsonResponse({ error: "Метод не поддерживается." }, 405);
      if (!currentUser.isAdmin) return jsonResponse({ error: "Доступно только администратору." }, 403);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      const vacancyId = Number(vacancyPathMatch[1]);
      if (request.method === "DELETE") {
        const deleted = await deleteVacancyRecord(env.DB, vacancyId);
        if (!deleted) return jsonResponse({ error: "Вакансия не найдена." }, 404);
        return jsonResponse(deleted);
      }
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Некорректные данные." }, 400);
      }
      try {
        const updated = await updateVacancy(env.DB, currentUser.name, vacancyId, payload);
        if (!updated) return jsonResponse({ error: "Вакансия не найдена." }, 404);
        return jsonResponse(updated);
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось обновить вакансию." }, error?.status || 400);
      }
    }

    if (url.pathname === "/api/tasks") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method === "GET") {
        const companyName = String(url.searchParams.get("company") || "").trim();
        const vacancyValue = url.searchParams.get("vacancy");
        const vacancyId = vacancyValue == null || vacancyValue === "" ? null : Number(vacancyValue);
        const companyNames = await readCompanyNames(env.DB);
        if (companyName && !companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
        if (vacancyId != null && !Number.isSafeInteger(vacancyId)) return jsonResponse({ error: "Некорректная вакансия." }, 400);
        const includeCompleted = url.searchParams.get("includeCompleted") === "1";
        const tasks=await readTasks(env.DB,currentUser.name,companyName,includeCompleted,vacancyId);
        return jsonResponse({tasks,entities:await readEntityDirectory(env.DB,tasks.map(t=>t.companyName))});
      }
      if (request.method === "POST") {
        const origin = request.headers.get("origin");
        if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
        let payload;
        try {
          payload = await request.json();
        } catch {
          return jsonResponse({ error: "Некорректные данные." }, 400);
        }
        const companyName = String(payload?.companyName || "").trim();
        const vacancyId = payload?.vacancyId == null || payload.vacancyId === "" ? null : Number(payload.vacancyId);
        const type = String(payload?.type || "");
        const note = String(payload?.note || "").trim();
        const dueAt = String(payload?.dueAt || "");
        const companyNames = await readCompanyNames(env.DB);
        if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
        if (vacancyId != null && !Number.isSafeInteger(vacancyId)) return jsonResponse({ error: "Некорректная вакансия." }, 400);
        if (!TASK_TYPES.includes(type)) return jsonResponse({ error: "Неизвестный тип задачи." }, 400);
        if (note.length > 1200) return jsonResponse({ error: "Примечание не должно превышать 1200 символов." }, 400);
        if (!dueAt || Number.isNaN(Date.parse(dueAt))) return jsonResponse({ error: "Некорректные дата и время." }, 400);
        try {
          return jsonResponse(await createTask(env, currentUser.name, companyName, vacancyId, type, note, new Date(dueAt).toISOString(), url.origin), 201);
        } catch (error) {
          return jsonResponse({ error: error?.message || "Не удалось создать задачу." }, 400);
        }
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    const taskPathMatch = url.pathname.match(/^\/api\/tasks\/(\d+)$/);
    if (taskPathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method !== "PUT") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Некорректные данные." }, 400);
      }
      if (payload && Object.hasOwn(payload,'dueAt')) {
        if(Object.hasOwn(payload,'completed'))return jsonResponse({error:'Изменяйте дату и статус отдельными запросами.'},400);
        const value=payload.dueAt;
        if(typeof value!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)||Number.isNaN(Date.parse(value))||new Date(value.slice(0,10)+'T00:00:00Z').toISOString().slice(0,10)!==value.slice(0,10))return jsonResponse({error:'Некорректные дата и время.'},400);
        try{
          const updated=await rescheduleTask(env,currentUser.name,Number(taskPathMatch[1]),new Date(value).toISOString(),url.origin);
          return updated?jsonResponse(updated):jsonResponse({error:'Задача не найдена.'},404);
        }catch{return jsonResponse({error:'Не удалось сохранить дату и время задачи.'},500);}
      }
      if (typeof payload?.completed !== "boolean") return jsonResponse({ error: "Некорректный статус задачи." }, 400);
      const updated = await setTaskCompletion(env.DB, currentUser.name, Number(taskPathMatch[1]), payload.completed);
      if (!updated) return jsonResponse({ error: "Задача не найдена." }, 404);
      return jsonResponse(updated);
    }

    if (url.pathname === "/api/applications/create-context") {
      if (request.method !== 'GET') return jsonResponse({error:'Метод не поддерживается.'},405);
      if (!env?.DB) return jsonResponse({error:'Хранилище CRM пока не подключено.'},503);
      try {
        return jsonResponse(await issueApplicationCreationContext(env.DB,currentUser.name,sessionSecret));
      } catch (error) {
        return jsonResponse({error:error?.message || 'Не удалось подготовить создание отклика.',code:error?.code},error?.status || 503);
      }
    }

    if (url.pathname === '/api/applications/create' || (url.pathname === '/api/applications' && request.method === 'POST')) {
      if (request.method !== 'POST') return jsonResponse({error:'Метод не поддерживается.'},405);
      if (!env?.DB) return jsonResponse({error:'Хранилище CRM пока не подключено.'},503);
      const origin=request.headers.get('origin');
      if (origin && origin !== url.origin) return jsonResponse({error:'Недопустимый источник запроса.'},403);
      let payload;
      try { payload=await request.json(); } catch { return jsonResponse({error:'Некорректные данные.'},400); }
      try {
        const result=await createApplicationRequest(env.DB,currentUser.name,sessionSecret,payload);
        return jsonResponse(result,result.replayed?200:201);
      } catch (error) {
        return jsonResponse({error:error?.message||'Не удалось создать отклик.',code:error?.code},error?.status||503);
      }
    }

    if (url.pathname === "/api/applications") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method === "GET") {
        const companyName = String(url.searchParams.get("company") || "").trim();
        const companyNames = await readCompanyNames(env.DB);
        if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
        return jsonResponse(await readCompanyApplications(env.DB, currentUser.name, companyName));
      }
      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    const applicationPathMatch = url.pathname.match(/^\/api\/applications\/(\d+)$/);
    if (applicationPathMatch) {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      if (request.method === "GET") {
        const [record] = await readApplicationRecords(env.DB, currentUser.name, "", Number(applicationPathMatch[1]));
        return record ? jsonResponse({record}) : jsonResponse({error: "Отклик не найден."}, 404);
      }
      if (request.method === "DELETE") {
        const deleted = await deleteCompanyApplication(env.DB, currentUser.name, Number(applicationPathMatch[1]));
        if (!deleted) return jsonResponse({ error: "Отклик не найден." }, 404);
        return jsonResponse(deleted);
      }
      if (request.method !== "PUT") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Некорректные данные." }, 400);
      }
      const status = String(payload?.status || "");
      const comment = String(payload?.comment || "");
      const vacancyId = payload?.vacancyId == null || payload.vacancyId === "" ? null : Number(payload.vacancyId);
      if (!CRM_STATUSES.includes(status)) return jsonResponse({ error: "Неизвестный статус отклика." }, 400);
      if (comment.length > 1000) return jsonResponse({ error: "Комментарий не должен превышать 1000 символов." }, 400);
      if (vacancyId != null && !Number.isSafeInteger(vacancyId)) return jsonResponse({ error: "Некорректная вакансия." }, 400);
      try {
        const recruiterContact = normalizeRecruiterContact(payload);
        const applicationSources = normalizeApplicationSources(payload);
        const updated = await updateCompanyApplication(env.DB, currentUser.name, Number(applicationPathMatch[1]), vacancyId, status, comment, recruiterContact, applicationSources, payload?.revision);
        if (!updated) return jsonResponse({ error: "Отклик не найден." }, 404);
        return jsonResponse(updated);
      } catch (error) {
        return jsonResponse({ error: error?.message || "Не удалось сохранить отклик." }, error?.status || 400);
      }
    }

    if (url.pathname === "/api/crm/applications") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const records=await readApplicationsCrm(env.DB,currentUser.name);
      return jsonResponse({records,entities:await readEntityDirectory(env.DB,records.map(r=>r.companyName))});
    }

    if (url.pathname === "/api/crm/favorites") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      return jsonResponse({ companyNames: await readFavoriteCompanyNames(env.DB, currentUser.name) });
    }

    if (url.pathname === "/api/crm/favorite") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      const companyName = String(url.searchParams.get("company") || "").trim();
      const companyNames = await readCompanyNames(env.DB);
      if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
      if (request.method !== "PUT") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Некорректные данные." }, 400);
      }
      if (typeof payload?.favorite !== "boolean") return jsonResponse({ error: "Некорректное значение избранного." }, 400);
      return jsonResponse(await saveCompanyFavorite(env.DB, currentUser.name, companyName, payload.favorite));
    }

    if (url.pathname === "/api/crm/hidden-companies") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      if (request.method !== "GET") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      return jsonResponse({ companyNames: await readHiddenCompanyNames(env.DB, currentUser.name) });
    }

    if (url.pathname === "/api/crm/hidden-company") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      const companyName = String(url.searchParams.get("company") || "").trim();
      const companyNames = await readCompanyNames(env.DB);
      if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);
      if (request.method !== "PUT") return jsonResponse({ error: "Метод не поддерживается." }, 405);
      const origin = request.headers.get("origin");
      if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
      let payload;
      try {
        payload = await request.json();
      } catch {
        return jsonResponse({ error: "Некорректные данные." }, 400);
      }
      if (typeof payload?.hidden !== "boolean") return jsonResponse({ error: "Некорректное значение скрытой компании." }, 400);
      return jsonResponse(await saveCompanyHidden(env.DB, currentUser.name, companyName, payload.hidden));
    }

    if (url.pathname === "/api/crm") {
      if (!env?.DB) return jsonResponse({ error: "Хранилище CRM пока не подключено." }, 503);
      const companyName = String(url.searchParams.get("company") || "").trim();
      const companyNames = await readCompanyNames(env.DB);
      if (!companyNames.has(companyName)) return jsonResponse({ error: "Компания не найдена." }, 404);

      if (request.method === "GET") {
        return jsonResponse(await readCompanyCrm(env.DB, currentUser.name, companyName));
      }

      if (request.method === "PUT") {
        const origin = request.headers.get("origin");
        if (origin && origin !== url.origin) return jsonResponse({ error: "Недопустимый источник запроса." }, 403);
        let payload;
        try {
          payload = await request.json();
        } catch {
          return jsonResponse({ error: "Некорректные данные." }, 400);
        }
        const status = String(payload?.status || "");
        const comment = String(payload?.comment || "");
        if (!CRM_STATUSES.includes(status)) return jsonResponse({ error: "Неизвестный статус отклика." }, 400);
        if (comment.length > 1000) return jsonResponse({ error: "Комментарий не должен превышать 1000 символов." }, 400);
        return jsonResponse(await saveCompanyCrm(env.DB, currentUser.name, companyName, status, comment));
      }

      return jsonResponse({ error: "Метод не поддерживается." }, 405);
    }

    if (url.pathname === "/" || url.pathname === "/index.html") {
      return null;
    }
    return new Response("Not found", { status: 404, headers: securityHeaders });
  }
};
