// Actual legacy snapshot selection; SQLite :memory: and synthetic values only.
import {DatabaseSync} from 'node:sqlite';
import {schema} from './historical-format-support.mjs';
import {currentRawSnapshot,snapshotMarkdown} from '../server/services/vacancy-fulltext.js';
import {candidateHash} from '../server/repositories/vacancy-candidates.js';
const cases=[
 {name:'inline',rows:[{id:'CI_A',created:'2026-01-01',raw:'CI_FULL_MARKDOWN',status:'valid'}]},
 {name:'parts',rows:[{id:'CI_A',created:'2026-01-01',parts:['CI_FULL_','MARKDOWN'],status:'valid'}]},
 {name:'latest_valid',rows:[{id:'CI_A',created:'2026-01-01',raw:'CI_OLD',status:'invalid'},{id:'CI_B',created:'2026-01-02',raw:'CI_NEW',status:'valid'}]},
 {name:'latest_invalid',rows:[{id:'CI_A',created:'2026-01-01',raw:'CI_OLD',status:'valid'},{id:'CI_B',created:'2026-01-02',raw:'CI_BAD',status:'invalid'}]},
 {name:'rabota_incomplete',source:'rabota',rows:[{id:'CI_A',created:'2026-01-01',raw:'CI_HEADER',status:'valid'}]},
 {name:'rabota_full',source:'rabota',rows:[{id:'CI_A',created:'2026-01-01',raw:'Обязанности: '+'CI_TEXT '.repeat(40),status:'valid'}]},
 {name:'empty',rows:[]}
];
for(const c of cases){
 const sqlite=new DatabaseSync(':memory:');for(const o of schema.objects)sqlite.exec(o.sql);
 const db={prepare(sql){let args=[];return {bind(...a){args=a;return this},async first(){return sqlite.prepare(sql).get(...args)??null},async all(){return{results:sqlite.prepare(sql).all(...args)}}}}};
 try{
  sqlite.prepare("INSERT INTO vacancy_candidates(id,creation_token,title_raw,title_hash,acquisition_method,first_seen_at,last_seen_at,created_at,updated_at) VALUES(990001,'CI','CI_TITLE',?,'manual','CI','CI','CI','CI')").run(await candidateHash('CI_TITLE'));
  for(const row of c.rows){const raw=row.raw??row.parts.join('');row.hash=await candidateHash(raw);sqlite.prepare("INSERT INTO vacancy_raw_snapshots(id,candidate_id,source_url,final_url,markdown_raw,content_hash,fetched_at,firecrawl_mode,content_status,diagnostics,created_at) VALUES(?,990001,'https://example.com/ci','https://example.com/ci',?,?,'CI','free',?,'{}',?)").run(row.id,row.raw??null,row.hash,row.status,row.created);for(const [i,part] of (row.parts??[]).entries())sqlite.prepare('INSERT INTO vacancy_raw_snapshot_parts(snapshot_id,part_index,markdown_raw) VALUES(?,?,?)').run(row.id,i,part)}
  const row=await currentRawSnapshot(db,{id:990001,vacancyUrl:'https://example.com/ci',sourceId:c.source||'companySite'});c.result=row?{id:row.id,markdown:await snapshotMarkdown(db,row),hash:row.content_hash,created:row.created_at}:null;
 }finally{sqlite.close()}
}
console.log(JSON.stringify(cases));
