import{createReadStream,writeFileSync}from'node:fs';import{createInterface}from'node:readline';
if(process.versions.node!=='24.19.0'||process.versions.icu!=='78.3'||process.versions.cldr!=='48.0'||process.versions.unicode!=='17.0')throw Error('Pinned Node/ICU/CLDR/Unicode versions required');
const ranges=[],exceptions={},nfd={},ccc=[],contractions=[],protectedKeys=[],reorder={},seenLead=new Map();let run=null,previous=-1,count=0;
const flush=()=>{if(run){ranges.push(run);run=null;}};
const cps=s=>Array.from(s,c=>c.codePointAt(0));
const keys=new Map();
for await(const line of createInterface({input:createReadStream(process.argv[2]),crlfDelay:Infinity})){
 const [cp,ces,cc,key]=JSON.parse(line);
 if(cp<0){const original=String.fromCharCode(...cc);if(JSON.stringify(ces)!==JSON.stringify(key)){const text=cps(original);if(text.some(c=>ccc.some(([p,w])=>p===c&&w!==0)))throw Error('Unsupported normalization-sensitive non-starter key');protectedKeys.push({text,ces});continue;}const text=original.normalize('NFD'),k=JSON.stringify(cps(text));if(keys.has(k)){if(JSON.stringify(keys.get(k))!==JSON.stringify(ces))throw Error('Normalized contraction conflict')}else{keys.set(k,ces);contractions.push({text:cps(text),ces})}continue;}
 count++;
 if(ces.length===1&&ces[0][0]){const p=ces[0][0];if(key.length>5)throw Error('Unexpected primary compression');const b=key.slice(0,-1);while(b.length<4)b.push(0);const mapped=b.reduce((s,n)=>s*256+n,0);if((p&0xffffff)!==(mapped&0xffffff))throw Error('Split script reorder');const lead=p>>>24,next=mapped>>>24;if(seenLead.has(lead)&&seenLead.get(lead)!==next)throw Error('Conflicting script reorder');seenLead.set(lead,next);if(lead!==next)reorder[lead]=next;}
 if(cc)ccc.push([cp,cc]);
 const text=String.fromCodePoint(cp),norm=text.normalize('NFD');
 if(text!==norm){flush();if(cp<0xac00||cp>0xd7a3)nfd[cp]=cps(norm);else{const i=cp-0xac00,w=[0x1100+Math.floor(i/588),0x1161+Math.floor(i%588/28)];if(i%28)w.push(0x11a7+i%28);if(JSON.stringify(cps(norm))!==JSON.stringify(w))throw Error('Hangul normalization drift')}continue;}
 if(ces.length!==1){flush();exceptions[cp]=ces;continue;}
 const [p,s,t]=ces[0];
 if(run&&run[1]===cp-1&&run[4]===s&&run[5]===t){const d=p-previous;if(run[0]===run[1]){run[3]=d;run[1]=cp;previous=p;continue;}if(d===run[3]){run[1]=cp;previous=p;continue;}}
 flush();run=[cp,cp,p,0,s,t];previous=p;
}
flush();if(count!==0x110000)throw Error('Incomplete scalar inventory');
const policy={node:process.versions.node,icu:process.versions.icu,cldr:process.versions.cldr,unicode:process.versions.unicode,locale:'ru',reorder,ranges,exceptions,nfd,ccc,contractions,protectedKeys};
const out=JSON.stringify(policy)+'\n';writeFileSync(process.argv[3],out);console.log(JSON.stringify({bytes:Buffer.byteLength(out),ranges:ranges.length,exceptions:Object.keys(exceptions).length,nfd:Object.keys(nfd).length,ccc:ccc.length,contractions:contractions.length,reorder}));
