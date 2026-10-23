// Server-only; extracted from the v218 Worker.


export const MAX_RESUME_LENGTH = 20000;

export const USER_PROFILE_TABLE_SQL = `CREATE TABLE IF NOT EXISTS user_profiles (
  user_name TEXT PRIMARY KEY NOT NULL,
  resume_md TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT user_profiles_resume_length CHECK(length(resume_md) <= 20000),
  CONSTRAINT user_profiles_revision CHECK(revision >= 0)
)`;

export async function readUserResume(db, userName) {
  await db.prepare(USER_PROFILE_TABLE_SQL).run();
  const row = await db.prepare('SELECT resume_md AS resumeMd, revision, updated_at AS updatedAt FROM user_profiles WHERE user_name = ?').bind(userName).first();
  return {...(row || {resumeMd:'', revision:0, updatedAt:null}), maxLength:MAX_RESUME_LENGTH};
}

export async function readResumeInput(request) {
  const limit = 256 * 1024;
  if (Number(request.headers.get('content-length') || 0) > limit) throw Object.assign(new Error('Запрос слишком большой.'), {status:413});
  if (!request.body) throw new Error('Передайте текст резюме.');
  const reader=request.body.getReader(), decoder=new TextDecoder();
  let size=0, text='';
  try {
    for (;;) {
      const {done,value}=await reader.read(); if(done) break;
      size+=value.byteLength;
      if(size>limit){await reader.cancel();throw Object.assign(new Error('Запрос слишком большой.'),{status:413});}
      text+=decoder.decode(value,{stream:true});
    }
    const payload=JSON.parse(text+decoder.decode());
    if (!payload || Array.isArray(payload) || Object.keys(payload).some(key => !['resumeMd','revision'].includes(key)) || typeof payload.resumeMd !== 'string') throw new Error('Передайте resumeMd и revision своего резюме.');
    if (payload.resumeMd.includes('\0') || Array.from(payload.resumeMd).length > MAX_RESUME_LENGTH) throw new Error('Резюме должно содержать не более 20 000 символов без нулевого символа.');
    if (!Number.isSafeInteger(payload.revision) || payload.revision < 0) throw new Error('Сначала загрузите текущую версию резюме.');
    return payload;
  } finally {reader.releaseLock();}
}

export async function saveUserResume(db, userName, input) {
  await db.prepare(USER_PROFILE_TABLE_SQL).run();
  const row=await db.prepare(`INSERT INTO user_profiles (user_name,resume_md,revision,updated_at)
    SELECT ?, ?, 1, CURRENT_TIMESTAMP WHERE ? = 0 OR EXISTS(SELECT 1 FROM user_profiles WHERE user_name = ? AND revision = ?)
    ON CONFLICT(user_name) DO UPDATE SET resume_md=excluded.resume_md, revision=user_profiles.revision+1, updated_at=CURRENT_TIMESTAMP
    WHERE user_profiles.revision = ?
    RETURNING resume_md AS resumeMd, revision, updated_at AS updatedAt`).bind(userName,input.resumeMd,input.revision,userName,input.revision,input.revision).first();
  if (!row) throw Object.assign(new Error('Резюме изменено в другой вкладке. Скопируйте свой текст и обновите страницу перед повторным сохранением.'),{status:409});
  return {...row,maxLength:MAX_RESUME_LENGTH};
}
