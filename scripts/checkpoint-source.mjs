// Source-only checkpoint on the explicitly requested refactor branch. Never deploys.
// Pass a short-lived Sites credential as one JSON line on hidden stdin, never argv/files.
import {spawn} from 'node:child_process';
import fs from 'node:fs';
const branch='refactor/react-typescript';
const line=await new Promise((resolve,reject)=>{let text='';if(process.stdin.isTTY)process.stdin.setRawMode(true);process.stderr.write('Ready for checkpoint credential on stdin (input is hidden).\n');process.stdin.setEncoding('utf8');process.stdin.on('data',chunk=>{text+=chunk;if(text.includes('\u0003'))reject(new Error('Cancelled'));if(text.includes('\n')){if(process.stdin.isTTY)process.stdin.setRawMode(false);process.stdin.pause();resolve(text);}});});
const credential=JSON.parse(line);const hosting=JSON.parse(fs.readFileSync('.openai/hosting.json','utf8'));
if(credential.auth_mode!=='http_extra_header'||!credential.token||credential.publish_on_push_accepted)throw new Error('An ordinary, non-publishing Sites credential is required.');
const url=new URL(credential.remote_url);if(url.protocol!=='https:'||url.username||url.password||!url.pathname.endsWith('/'+hosting.project_id+'.git'))throw new Error('Credential does not match this Site.');
if(Date.parse(credential.token_expires_at)<=Date.now())throw new Error('Renew expired credential.');
async function git(args,network=false){const env={...process.env,GIT_TERMINAL_PROMPT:'0'};for(const key of Object.keys(env))if(key.startsWith('GIT_TRACE')||key==='GIT_CURL_VERBOSE')delete env[key];let auth=[];if(network){env.SITES_GIT_AUTHORIZATION='Authorization: Bearer '+credential.token;auth=['-c','credential.helper=','-c','http.extraHeader=','-c','http.followRedirects=false','--config-env=http.'+credential.remote_url+'.extraHeader=SITES_GIT_AUTHORIZATION'];}return await new Promise((resolve,reject)=>{const child=spawn('git',[...auth,...args],{env,stdio:['ignore','pipe','pipe']});let out='',err='';child.stdout.on('data',b=>out+=b);child.stderr.on('data',b=>err+=b);child.on('error',reject);child.on('close',code=>code===0?resolve(out.trim()):reject(new Error(err.split(credential.token).join('[redacted]'))));});}
if(await git(['branch','--show-current'])!==branch)throw new Error('Checkout must be on '+branch);
if(await git(['status','--porcelain']))throw new Error('Commit the completed step before pushing.');
const sha=await git(['rev-parse','HEAD']);await git(['push',credential.remote_url,sha+':refs/heads/'+branch],true);
const remote=(await git(['ls-remote','--heads',credential.remote_url,'refs/heads/'+branch],true)).split(/\s+/)[0];
if(remote!==sha)throw new Error('Remote checkpoint verification failed.');
console.log(JSON.stringify({branch,commit_sha:sha,remote_verified:true,deployed:false}));
