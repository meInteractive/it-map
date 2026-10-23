export const PARSER_ERROR_HISTORY_SQL=`CREATE TABLE IF NOT EXISTS parser_error_daily (
 owner_name TEXT NOT NULL,
 day TEXT NOT NULL CHECK(length(day)=10 AND day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
 p1_peak INTEGER NOT NULL CHECK(p1_peak>=0),
 p2_peak INTEGER NOT NULL CHECK(p2_peak>=0),
 first_sample_at INTEGER NOT NULL,
 last_sample_at INTEGER NOT NULL,
 PRIMARY KEY(owner_name,day)
)`;

// Modern Moscow civil dates are UTC+3. Unknown days remain gaps, never zeros.
const dayAt=now=>new Date(now+3*3600000).toISOString().slice(0,10);
export async function recordParserErrorSnapshot(db,owner,counts,now=Date.now()){
 await db.prepare(`INSERT INTO parser_error_daily(owner_name,day,p1_peak,p2_peak,first_sample_at,last_sample_at)
 VALUES(?,?,?,?,?,?) ON CONFLICT(owner_name,day) DO UPDATE SET
 p1_peak=MAX(p1_peak,excluded.p1_peak),p2_peak=MAX(p2_peak,excluded.p2_peak),
 first_sample_at=MIN(first_sample_at,excluded.first_sample_at),last_sample_at=MAX(last_sample_at,excluded.last_sample_at)`)
 .bind(owner,dayAt(now),counts.p1,counts.p2,now,now).run();
}
export async function readParserErrorHistory(db,owner,now=Date.now()){
 const days=30,keys=Array.from({length:days},(_,i)=>dayAt(now-(days-1-i)*86400000));
 const rows=(await db.prepare('SELECT day,p1_peak,p2_peak,last_sample_at FROM parser_error_daily WHERE owner_name=? AND day>=? AND day<=? ORDER BY day').bind(owner,keys[0],keys.at(-1)).all()).results||[];
 const byDay=new Map(rows.map(r=>[r.day,r]));
 return {timezone:'Europe/Moscow',metric:'daily_peak_active',days,observedDays:rows.length,
  average:{p1:rows.length?rows.reduce((n,r)=>n+r.p1_peak,0)/rows.length:null,p2:rows.length?rows.reduce((n,r)=>n+r.p2_peak,0)/rows.length:null},
  points:keys.map(day=>{const r=byDay.get(day);return {day,p1:r?.p1_peak??null,p2:r?.p2_peak??null,observedAt:r?.last_sample_at??null};})};
}
