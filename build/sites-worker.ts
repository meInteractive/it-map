import handler from 'vinext/server/fetch-handler';
import backend from '../server/routes/index.js';
import {securityHeaders} from '../server/http.js';
export default {async fetch(request:Request,env:Record<string,unknown>,ctx:ExecutionContext):Promise<Response>{
 const result=await backend.fetch(request,env,ctx);
 const failedLogin=result?.headers.get('x-app-login-error')==='1';
 if(result&&!failedLogin)return result;
 const url=new URL(request.url);const pageRequest=failedLogin||url.pathname==='/index.html'?new Request(new URL('/',url),{headers:request.headers}):request;
 const page=await handler.fetch(pageRequest,env,ctx);
 const headers=new Headers(page.headers);Object.entries(securityHeaders).forEach(([k,v])=>headers.set(k,String(v)));
 return new Response(page.body,{status:failedLogin?401:page.status,headers});
}};
