import{readFileSync,writeFileSync}from'node:fs';import{gzipSync}from'node:zlib';import{createHash}from'node:crypto';
const p=JSON.parse(readFileSync(process.argv[2]));const out=[...Buffer.from('ICURU78\x01')];
function uv(n){let v=BigInt(n);if(v<0)throw Error('Negative unsigned');while(v>=128n){out.push(Number(v&127n)|128);v>>=7n;}out.push(Number(v));}
const zig=n=>n<0?BigInt(-n)*2n-1n:BigInt(n)*2n;
function ces(values){uv(values.length);for(const ce of values)for(const w of ce)uv(w);}
for(const n of [p.ranges.length,Object.keys(p.exceptions).length,Object.keys(p.nfd).length,p.ccc.length,p.contractions.length,p.protectedKeys.length,Object.keys(p.reorder).length])uv(n);
let prev=-1;for(const [s,e,v,d,sec,t]of p.ranges){uv(s-prev-1);uv(e-s);uv(v);uv(zig(d));uv(sec);uv(t);prev=e;}
prev=-1;for(const [cp,values]of Object.entries(p.exceptions)){uv(Number(cp)-prev-1);ces(values);prev=Number(cp);}
prev=-1;for(const [cp,values]of Object.entries(p.nfd)){uv(Number(cp)-prev-1);uv(values.length);for(const v of values)uv(v);prev=Number(cp);}
prev=-1;for(const [cp,cc]of p.ccc){uv(cp-prev-1);uv(cc);prev=cp;}
for(const list of [p.contractions,p.protectedKeys])for(const {text,ces:values}of list){uv(text.length);for(const v of text)uv(v);ces(values);}
for(const [before,after]of Object.entries(p.reorder)){uv(before);uv(after);}
const data=Buffer.from(out),packed=gzipSync(data,{level:9}),base64=packed.toString('base64');const document={nodeMajor:24,icu:p.icu,cldr:p.cldr,unicode:p.unicode,locale:'ru',format:'icu-ru-weights-v1',sha256:createHash('sha256').update(data).digest('hex'),data:base64.match(/.{1,72}/g).join('\n')};
writeFileSync(process.argv[3],JSON.stringify(document)+'\n');console.log(JSON.stringify({raw:data.length,gzip:packed.length,json:JSON.stringify(document).length,ranges:p.ranges.length,exceptions:Object.keys(p.exceptions).length,protected:p.protectedKeys.length}));
