import {ensureCrmSchema} from './schema.js';
import {readVacancyRoleCatalog} from './vacancy-roles.js';
import {readPromptInput} from './prompts.js';

const PREVIOUS_CLASSIFICATION_DEFAULTS = {
 'root-it-manager': `Определи по названию вакансии, относится ли она к управлению IT-разработкой, IT-проектами, цифровыми продуктами или инженерными командами.

Подходящие направления:
- Руководитель разработки, Head of Development.
- Руководитель инженерной команды, Head of Engineering.
- Engineering Manager, Development Manager.
- Руководитель IT-проектов, Senior IT Project Manager.
- Technical Project Manager, Technical Program Manager.
- Program Manager, Program Director.
- Delivery Manager, Head of Delivery.
- Product Manager, Senior Product Manager.
- Head of Product, Product Lead.
- Platform Product Manager.
- Другие близкие управленческие роли, связанные с разработкой программного обеспечения, IT-проектами и цифровыми продуктами.

Не подходят:
- Рядовые разработчики без управленческих обязанностей.
- Тестировщики и QA без руководящих функций.
- Системные администраторы и DevOps без управления разработкой.
- Дизайнеры без управления цифровым продуктом.
- Менеджеры проектов, не связанных с IT.
- Менеджеры продаж, маркетинга и HR.
- Административные руководители вне IT.

Правила:
1. Оценивай смысл названия, а не буквальное совпадение слов.
2. Русские и английские названия равнозначны.
3. Не придумывай обязанности, которых нет в заголовке.
4. Если название неоднозначно, но потенциально относится к управлению IT-разработкой, IT-проектами или цифровыми продуктами, выбирай «Подходит».
5. Не оценивай зарплату, работодателя или опыт кандидата. На этом этапе используется только заголовок вакансии.

Результат классификации: match или reject.`,
 'root-journalist': `Определи по названию вакансии, относится ли она к профессиональной журналистике, редакторской работе или созданию журналистских материалов.

Подходящие направления:
- Редактор.
- Выпускающий редактор.
- Шеф-редактор.
- Главный редактор.
- Редактор новостей.
- Редактор СМИ.
- Редактор медиа.
- Журналист.
- Корреспондент.
- Экономический журналист.
- Деловой журналист.
- Дата-журналист, Data Journalist.
- Журналист-аналитик.
- Другие близкие специальности, связанные с профессиональной журналистикой и редакционной работой.

Не подходят:
- Чистый рекламный копирайтинг.
- SMM и продвижение в социальных сетях без журналистской составляющей.
- PR и корпоративные коммуникации без редакционной работы.
- Маркетинговые должности.
- Создание рекламных текстов без журналистских задач.
- Другие должности, не связанные с журналистикой и редактурой.

Правила:
1. Оценивай смысл названия.
2. Учитывай русские и английские эквиваленты.
3. Не придумывай дополнительные обязанности.
4. Если название неоднозначно, но потенциально относится к профессиональной журналистике или редакторской работе, выбирай «Подходит».
5. Не оценивай зарплату, работодателя и квалификацию кандидата. Используй только заголовок вакансии.

Результат классификации: match или reject.`
};

export const CLASSIFICATION_DEFAULTS={...PREVIOUS_CLASSIFICATION_DEFAULTS,'root-it-manager':PREVIOUS_CLASSIFICATION_DEFAULTS['root-it-manager'].replace('- Head of Product, Product Lead.','- Head of Product, Product Lead, Product Owner.\n- Лидер продукта, владелец продукта, руководитель продукта, руководитель продуктового направления.').replace('3. Не придумывай обязанности, которых нет в заголовке.','3. Не придумывай обязанности, которых нет в заголовке. Название «лидер продукта» означает продуктовую управленческую роль и не требует буквальных слов IT или Manager. Не отклоняй цифровой продукт только потому, что в заголовке нет слова «разработка».')};

export async function readClassificationSettings(db) {
 await ensureCrmSchema(db);
 const catalog=await readVacancyRoleCatalog(db);
 // Only the immutable stable keys seed defaults. Renames/moves never re-key a
 // prompt, and INSERT OR IGNORE never overwrites even a deliberately empty one.
 if(catalog.roots.length)await db.batch(catalog.roots.map(role=>db.prepare(
  'INSERT OR IGNORE INTO vacancy_role_classification(role_id,markdown,revision,parsing_enabled) VALUES(?,?,?,?)'
 ).bind(role.id,CLASSIFICATION_DEFAULTS[role.stableKey]||'',CLASSIFICATION_DEFAULTS[role.stableKey]?1:0,CLASSIFICATION_DEFAULTS[role.stableKey]?1:0)));
 // Upgrade only the exact previous standard text; never replace an owner's edits.
 await db.prepare("UPDATE vacancy_role_classification SET markdown=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE markdown=? AND role_id IN (SELECT id FROM vacancy_roles WHERE stable_key='root-it-manager' AND parent_id IS NULL)").bind(CLASSIFICATION_DEFAULTS['root-it-manager'],PREVIOUS_CLASSIFICATION_DEFAULTS['root-it-manager']).run();
 const rows=(await db.prepare('SELECT role_id AS roleId,markdown,revision,parsing_enabled AS enabled,updated_at AS updatedAt FROM vacancy_role_classification').all()).results||[];
 const byId=new Map(rows.map(row=>[Number(row.roleId),row]));
 return {maxLength:10000,roles:catalog.roots.map(role=>({...byId.get(role.id),roleId:role.id,name:role.name,enabled:!!byId.get(role.id)?.enabled,defaultMarkdown:CLASSIFICATION_DEFAULTS[role.stableKey]||null,descendants:catalog.roles.filter(item=>role.descendantIds.includes(item.id)).map(({id,path})=>({id,path}))}))};
}

export const readClassificationPromptInput=request=>readPromptInput(request,'title_classification');

export async function readClassificationInput(request) {
 if(Number(request.headers.get('content-length')||0)>32768)throw Object.assign(new Error('Запрос слишком большой.'),{status:413});
 if(!request.body)throw new Error('Передайте данные запроса.');
 const reader=request.body.getReader(),chunks=[];let size=0;
 try{for(;;){const {done,value}=await reader.read();if(done)break;size+=value.byteLength;if(size>32768){await reader.cancel();throw Object.assign(new Error('Запрос слишком большой.'),{status:413});}chunks.push(value);}}
 finally{reader.releaseLock();}
 const bytes=new Uint8Array(size);let offset=0;for(const chunk of chunks){bytes.set(chunk,offset);offset+=chunk.byteLength;}
 const input=JSON.parse(new TextDecoder().decode(bytes));
 if(!input||typeof input!=='object'||Array.isArray(input))throw new Error('Некорректные данные.');
 return input;
}

export async function saveClassificationEnabled(db,roleId,input) {
 if(Object.keys(input).some(key=>!['enabled','revision'].includes(key))||typeof input.enabled!=='boolean'||!Number.isSafeInteger(input.revision)||input.revision<0)throw new Error('Передайте enabled и текущую revision.');
 await readClassificationSettings(db);
 const row=await db.prepare(`UPDATE vacancy_role_classification SET parsing_enabled=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP
  WHERE role_id=? AND revision=? AND EXISTS(SELECT 1 FROM vacancy_roles WHERE id=? AND parent_id IS NULL)
  RETURNING role_id AS roleId,revision,parsing_enabled AS enabled,updated_at AS updatedAt`).bind(input.enabled?1:0,roleId,input.revision,roleId).first();
 if(!row)throw Object.assign(new Error('Настройки или роль изменены. Обновите страницу.'),{status:409});
 return {...row,enabled:!!row.enabled};
}

export async function saveClassificationPrompt(db,roleId,input) {
 await readClassificationSettings(db);
 const row=await db.prepare(`UPDATE vacancy_role_classification SET markdown=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP
  WHERE role_id=? AND revision=? AND EXISTS(SELECT 1 FROM vacancy_roles WHERE id=? AND parent_id IS NULL)
  RETURNING role_id AS roleId,markdown,revision,updated_at AS updatedAt`).bind(input.markdown,roleId,input.revision,roleId).first();
 if(!row)throw Object.assign(new Error('Роль или промт изменены. Скопируйте текст и обновите настройки перед повторным сохранением.'),{status:409});
 return row;
}

export async function activeClassificationRoles(db) {
 const settings=await readClassificationSettings(db);
 return settings.roles.filter(role=>role.enabled&&role.markdown.trim()).map(({roleId,name,markdown,revision})=>({roleId,name,markdown,revision}));
}
