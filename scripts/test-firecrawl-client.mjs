import assert from 'node:assert/strict';
import {firecrawlScrape,testFirecrawlConnection,safeListingUrl,preflightListingUrl} from '../server/integrations/firecrawl-transport.js';
const key='fc-SYNTHETIC-FIRECRAWL-SECRET';let requests=[];
const fetcher=async(url,options)=>{requests.push({url,options});return Response.json({success:true,data:url.endsWith('credit-usage')?{remainingCredits:20}:{markdown:'[Архитектор платформы](/jobs/42)',links:['/jobs/42'],metadata:{statusCode:200,url:'https://careers.real.org/jobs',creditsUsed:1}}});};
const r=await firecrawlScrape(key,'https://careers.real.org/jobs',{hosts:['careers.real.org'],fetcher});assert.equal(r.creditsReported,1);assert.equal(r.targetStatus,200);
const body=JSON.parse(requests[0].options.body);assert.deepEqual(body.formats,['markdown','links']);assert.equal(body.proxy,'basic');assert.deepEqual(body.parsers,[]);assert.equal(body.skipTlsVerification,false);assert.equal(body.maxAge,0);assert.ok(!JSON.stringify(r).includes(key));
assert.equal((await testFirecrawlConnection(key,{fetcher})).remainingCredits,20);assert.equal(requests.at(-1).options.method,'GET');
for(const url of ['http://localhost/a','http://127.1/a','http://0x7f000001/a','http://[::1]/a','http://metadata.google.internal/a','http://192.168.0.1/a','https://user:pass@careers.real.org/a','ftp://careers.real.org/a','https://internal/a','https://bad%00.domain.org/a','https://careers.real.org:443/a']){
 if(url.endsWith(':443/a'))continue;assert.throws(()=>safeListingUrl(url));
}
assert.throws(()=>safeListingUrl('https://evil.real.org/jobs',['careers.real.org']));
for(const [status,code] of [[401,'auth'],[402,'credits'],[429,'rate'],[403,'blocked'],[500,'provider']]){
 let count=0;await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{fetcher:async()=>{count++;return Response.json({error:'secret '+key},{status});}}),e=>e.code===code&&!e.message.includes(key));assert.equal(count,1);
}
let attempts=0;await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{budget:{remaining:2},fetcher:async()=>{attempts++;return Response.json({success:false},{status:503,headers:{'retry-after':'0'}});}}),e=>e.code==='provider'&&e.requests===2);assert.equal(attempts,2);
for(const [status,markdown,code] of [[404,'Page missing','target'],[403,'Protected','blocked'],[200,'Verify you are human','blocked']])await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{fetcher:async()=>Response.json({success:true,data:{markdown,metadata:{statusCode:status}}})}),e=>e.code===code&&e.requests===1);
await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{hosts:['careers.real.org'],fetcher:async()=>Response.json({success:true,data:{metadata:{url:'http://127.0.0.1/',statusCode:200}}})}),e=>e.code==='unsafe_url');
await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{proxy:'auto',fetcher}),e=>e.status===400);
await assert.rejects(()=>firecrawlScrape(key,'https://careers.real.org/jobs',{proxy:'enhanced',fetcher}),e=>e.status===400);
let targetCalls=0;await assert.rejects(()=>preflightListingUrl('https://careers.real.org/jobs',['careers.real.org'],{fetcher:async(url)=>{if(url.startsWith('https://cloudflare-dns.com'))return Response.json({Status:0,Answer:[{type:1,data:'10.0.0.1'}]});targetCalls++;return new Response(null);}}),e=>e.code==='unsafe_url');assert.equal(targetCalls,0);
await assert.rejects(()=>preflightListingUrl('https://careers.real.org/jobs',['careers.real.org'],{fetcher:async(url)=>url.startsWith('https://cloudflare-dns.com')?Response.json({Status:0,Answer:[{type:1,data:'93.184.216.34'}]}):new Response(null,{status:302,headers:{location:'https://unapproved.real.org/jobs'}})}),e=>e.code==='redirect');
console.log('Firecrawl client: formats, no AI/proxy fallback, usage check, budget/retry, auth/credits/rate/target/blocking, secret redaction, URL/DNS/redirect bounds passed');
