// Public Unicode/synthetic oracle. Uses the unchanged Node collator plus the
// explicitly accepted canonical-equivalence exception; no network or SQL.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {gunzipSync} from 'node:zlib';
assert.equal(process.versions.node,'24.19.0');assert.equal(process.versions.icu,'78.3');
assert.equal(process.versions.cldr,'48.0');assert.equal(process.versions.unicode,'17.0');
globalThis.fetch=()=>{throw Error('Collation oracle forbids network');};
const doc=JSON.parse(readFileSync(new URL('../internal/rucollation/weights.json',import.meta.url)));
const buf=gunzipSync(Buffer.from(doc.data,'base64'));let off=8;
function uv(){let n=0,m=1,b;do{b=buf[off++];assert.ok(b!==undefined);n+=(b&127)*m;m*=128;}while(b&128);return n;}
const counts=Array.from({length:7},uv),skipces=()=>{const n=uv();for(let i=0;i<n*3;i++)uv();};
for(let i=0;i<counts[0]*6;i++)uv();for(let i=0;i<counts[1];i++){uv();skipces();}
for(let i=0;i<counts[2];i++){uv();const n=uv();for(let j=0;j<n;j++)uv();}
let cp=-1;const ccc=[];for(let i=0;i<counts[3];i++){cp+=uv()+1;ccc.push(cp);uv();}
const contractions=[];for(let i=0;i<counts[4]+counts[5];i++){const n=uv();contractions.push(Array.from({length:n},uv));skipces();}
const pairs=[],marks=['\u0300','\u0301','\u0306','\u0327','\u034f','\u035c','\u200d','\u200c','\0'];
for(const text of contractions){const s=String.fromCodePoint(...text),parts=Array.from(s),variants=[s,s.normalize('NFC'),s.normalize('NFD'),s+s,'X'+s,s+'X'];for(const m of marks){variants.push(parts[0]+m+parts.slice(1).join(''));variants.push(s+m);}for(let i=0;i<variants.length;i++)for(let j=i+1;j<variants.length;j++)pairs.push([variants[i],variants[j]]);}
let seed=0x814edc03;const rand=()=>{seed^=seed<<13;seed^=seed>>>17;seed^=seed<<5;return seed>>>0},pick=a=>a[rand()%a.length];
for(const cp of ccc){const m=String.fromCodePoint(cp);for(const root of ['и','И','е','Е'])for(const extra of ['\u0306','\u0308']){pairs.push([root+m+extra,root+extra+m],[root+m+extra,(root+m+extra).normalize('NFC')]);}}
for(let i=0;i<30000;i++){const base=String.fromCodePoint(rand()%0x110000),tail=pick(marks)+pick(marks)+String.fromCodePoint(rand()%0x110000),s=base+tail;pairs.push([s,s.normalize('NFD')],[s,s.toLocaleUpperCase('ru')],[s.normalize('NFC'),s.toLocaleLowerCase('ru')]);}
// Unrelated names exercise ordering, rather than only normalized variants of
// one string. Include supplementary code points and invalid lone UTF16 units.
const common=['А','а','Е','Ё','Я','я','A','a','Z','0','2','20','😀','\ud800','\udfff','\0','é','и\u0306'];
for(const a of common)for(const b of common)pairs.push([a,b]);
for(let i=0;i<60000;i++){
 const make=()=>{let s='';for(let j=0,n=1+rand()%8;j<n;j++)s+=rand()%2?pick(common):String.fromCodePoint(rand()%0x110000);return s;};
 pairs.push([make(),make()]);
}
const col=new Intl.Collator('ru'),units=s=>Array.from({length:s.length},(_,i)=>s.charCodeAt(i));let differences=0;
for(const [a,b]of pairs){const source=Math.sign(col.compare(a,b)),equiv=a.normalize('NFD')===b.normalize('NFD');const exception=source!==0&&equiv&&/^[иИ]/u.test(a)&&/[\u{1E4EC}\u{1E4ED}]/u.test(a);const canonical=exception?0:source;if(exception)differences++;process.stdout.write(JSON.stringify({a:units(a),b:units(b),source,canonical,exception})+'\n');}
assert.equal(pairs.length,479072);assert.equal(differences,8);
