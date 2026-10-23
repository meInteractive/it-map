// Operator smoke check. Credentials arrive only through hidden TTY stdin and
// are sent only to the exact Site origin supplied by its owner tooling.
import crypto from 'node:crypto';
if(!process.stdin.isTTY)throw new Error('A hidden TTY input is required.');
process.stdin.setRawMode(true);process.stdin.resume();process.stdout.write('Ready for hidden Site probe JSON on stdin.\n');
const input=await new Promise((resolve,reject)=>{
 let text='',done=false;
 process.stdin.on('data',part=>{
  if(done)return;
  text+=part.toString();
  if(!text.includes('\n'))return;
  done=true;process.stdin.pause();process.stdin.setRawMode(false);
  try{resolve(JSON.parse(text.trim()))}catch{reject(new Error('Invalid input'))}
 });
});
try{
 const origin=new URL(input.url);if(origin.protocol!=='https:'||origin.pathname!=='/'||origin.search||origin.username||origin.password)throw new Error('Use the exact HTTPS Site origin.');
 const requestId=input.requestId||crypto.randomUUID(),headers={'content-type':'application/json',authorization:'Bearer '+input.token};if(input.bypass)headers['OAI-Sites-Authorization']='Bearer '+input.bypass;
 const response=await fetch(new URL('/api/internal/scheduler/tick',origin),{method:'POST',headers,body:JSON.stringify({requestId,diagnostic:true}),signal:AbortSignal.timeout(25000)});
 const body=await response.json();if(!response.ok)process.stdout.write(JSON.stringify({status:response.status,layer:body.error==='Недействительный ключ диспетчера.'?'application_token':'platform_or_other',authenticate:!!response.headers.get('www-authenticate')})+'\n');if(!response.ok||body.job?.status!=='completed'||body.externalRequests!==0)throw new Error('Server did not confirm a completed no-cost probe ('+response.status+').');
 process.stdout.write(JSON.stringify({status:response.status,requestId,jobId:body.job.id,jobStatus:body.job.status,runId:body.run.id,externalRequests:body.externalRequests})+'\n');
}catch(e){process.stderr.write('Hosted scheduler probe failed: '+e.message+'\n');process.exitCode=1;}
