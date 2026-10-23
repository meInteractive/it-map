// Server-only; extracted from the v218 Worker.
import {encoder,AGENCY_RATING_INPUT_META,AGENCY_TYPE_OPTIONS,AGENCY_FUNCTION_OPTIONS,AGENCY_POSITION_LEVEL_OPTIONS,AGENCY_EXECUTIVE_SEARCH_OPTIONS,AGENCY_CLOSED_VACANCIES_OPTIONS,AGENCY_VACANCY_PUBLICITY_OPTIONS,AGENCY_EMPLOYER_DISCLOSURE_OPTIONS,AGENCY_SALARY_DISCLOSURE_OPTIONS,AGENCY_PRIMARY_CHANNEL_OPTIONS,AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS,AGENCY_VACANCY_FRESHNESS_OPTIONS,AGENCY_ACCEPTS_CV_OPTIONS,AGENCY_CONTACT_METHOD_OPTIONS,VACANCY_EXPORT_HEADERS} from '../constants.js';
import {ensureCrmSchema} from '../repositories/schema.js';
import {readAppCatalog,ensureCompanyIdentities,ensureAgencyIdentities} from '../repositories/catalog.js';

export function xmlEscape(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

export function xlsxColumnName(index) {
  let result = "";
  let current = index;
  while (current > 0) {
    const remainder = (current - 1) % 26;
    result = String.fromCharCode(65 + remainder) + result;
    current = Math.floor((current - 1) / 26);
  }
  return result;
}

export function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return (crc ^ 0xffffffff) >>> 0;
}

export function uint16(value) {
  return new Uint8Array([value & 255, (value >>> 8) & 255]);
}

export function uint32(value) {
  return new Uint8Array([value & 255, (value >>> 8) & 255, (value >>> 16) & 255, (value >>> 24) & 255]);
}

export function joinBytes(parts) {
  const length = parts.reduce((sum, part) => sum + part.length, 0);
  const result = new Uint8Array(length);
  let offset = 0;
  for (const part of parts) {
    result.set(part, offset);
    offset += part.length;
  }
  return result;
}

export function zipStored(files) {
  const encoder = new TextEncoder();
  const entries = files.map(([name, content]) => ({ name: encoder.encode(name), content: encoder.encode(content) }));
  const localParts = [];
  const centralParts = [];
  let offset = 0;
  for (const entry of entries) {
    const checksum = crc32(entry.content);
    const local = joinBytes([
      uint32(0x04034b50), uint16(20), uint16(0), uint16(0), uint16(0), uint16(0), uint32(checksum),
      uint32(entry.content.length), uint32(entry.content.length), uint16(entry.name.length), uint16(0), entry.name, entry.content
    ]);
    localParts.push(local);
    centralParts.push(joinBytes([
      uint32(0x02014b50), uint16(20), uint16(20), uint16(0), uint16(0), uint16(0), uint16(0), uint32(checksum),
      uint32(entry.content.length), uint32(entry.content.length), uint16(entry.name.length), uint16(0), uint16(0), uint16(0), uint16(0),
      uint32(0), uint32(offset), entry.name
    ]));
    offset += local.length;
  }
  const central = joinBytes(centralParts);
  return joinBytes([
    ...localParts,
    central,
    uint32(0x06054b50), uint16(0), uint16(0), uint16(entries.length), uint16(entries.length), uint32(central.length), uint32(offset), uint16(0)
  ]);
}

export function xlsxCell(reference, value, style = 0, numeric = false) {
  const styleAttribute = style ? ` s="${style}"` : "";
  if (numeric) return `<c r="${reference}"${styleAttribute} t="n"><v>${Number(value)}</v></c>`;
  return `<c r="${reference}"${styleAttribute} t="inlineStr"><is><t xml:space="preserve">${xmlEscape(value)}</t></is></c>`;
}

export function companyVacancySourcesXlsx(rows) {
  const headers = ["ID компании в БД", "Место в рейтинге", "Название компании", "Избранная", "Скрытая", "Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до", "Сайт компании", "HH", "Хабр", "Getmatch", "Telegram", "DreamJob", "Эйч", "rabota.ru"];
  const worksheetRows = [headers, ...rows.map((row) => [
    row.id, row.rank, row.name, row.favorite ? "да" : "", row.hidden ? "да" : "",
    row.middle?.[0] ?? "", row.middle?.[1] ?? "", row.senior?.[0] ?? "", row.senior?.[1] ?? "",
    row.lead?.[0] ?? "", row.lead?.[1] ?? "", row.head?.[0] ?? "", row.head?.[1] ?? "",
    row.companySite, row.hh, row.habr, row.getmatch, row.telegram, row.dreamJob, row.hCareers, row.rabota
  ])].map((values, rowIndex) => `<row r="${rowIndex + 1}"${rowIndex === 0 ? ' ht="30" customHeight="1"' : ""}>${values.map((value, columnIndex) =>
    xlsxCell(`${xlsxColumnName(columnIndex + 1)}${rowIndex + 1}`, value, rowIndex === 0 ? 1 : 0, rowIndex > 0 && (columnIndex === 0 || columnIndex === 1 || (columnIndex >= 5 && columnIndex <= 12)) && value !== "")
  ).join("")}</row>`).join("");
  const sheet = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="2" width="18" customWidth="1"/><col min="3" max="3" width="30" customWidth="1"/><col min="4" max="5" width="13" customWidth="1"/><col min="6" max="13" width="16" customWidth="1"/><col min="14" max="21" width="54" customWidth="1"/></cols><sheetData>${worksheetRows}</sheetData><autoFilter ref="A1:U${rows.length + 1}"/></worksheet>`;
  return zipStored([
    ["[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`],
    ["_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`],
    ["xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Компании" sheetId="1" r:id="rId1"/></sheets></workbook>`],
    ["xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`],
    ["xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F2937"/><bgColor rgb="FF1F2937"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs></styleSheet>`],
    ["xl/worksheets/sheet1.xml", sheet]
  ]);
}

export function companyReviewSourcesXlsx(rows) {
  const headers = ["ID компании в БД", "Место в рейтинге", "Название компании", "Избранная", "Скрытая", "Habr", "DreamJob", "GlassDoor", "SuperJob", "Правда Сотрудников", "Nahjob", "BossNadzor", "YandexMap", "Plus Short", "Minus Short", "Plus", "Minus"];
  const worksheetRows = [headers, ...rows.map((row) => [
    row.id, row.rank, row.name, row.favorite ? "да" : "", row.hidden ? "да" : "",
    row.habr, row.dreamJob, row.glassDoor, row.superJob, row.pravdaSotrudnikov, row.nahjob, row.bossNadzor, row.yandexMap,
    row.plusShort, row.minusShort, row.plus, row.minus
  ])].map((values, rowIndex) => `<row r="${rowIndex + 1}"${rowIndex === 0 ? ' ht="30" customHeight="1"' : ""}>${values.map((value, columnIndex) =>
    xlsxCell(`${xlsxColumnName(columnIndex + 1)}${rowIndex + 1}`, value, rowIndex === 0 ? 1 : 0, rowIndex > 0 && (columnIndex === 0 || columnIndex === 1) && value !== "")
  ).join("")}</row>`).join("");
  const sheet = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="2" width="18" customWidth="1"/><col min="3" max="3" width="30" customWidth="1"/><col min="4" max="5" width="13" customWidth="1"/><col min="6" max="13" width="54" customWidth="1"/><col min="14" max="17" width="60" customWidth="1"/></cols><sheetData>${worksheetRows}</sheetData><autoFilter ref="A1:Q${rows.length + 1}"/></worksheet>`;
  return zipStored([
    ["[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`],
    ["_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`],
    ["xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Отзывы" sheetId="1" r:id="rId1"/></sheets></workbook>`],
    ["xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`],
    ["xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F2937"/><bgColor rgb="FF1F2937"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs></styleSheet>`],
    ["xl/worksheets/sheet1.xml", sheet]
  ]);
}

export const COMPANY_IMPORT_EXPORT_HEADERS = [
  "ID компании", "Название компании", "Отрасль", "Подходящие роли / направления", "Итоговая оценка",
  "Рейтинг сотрудников", "Work-life balance", "Балл WLB", "Уверенность в данных", "Риски / что проверить",
  "Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до",
  "IT-аккредитация / отсрочка", "Мобилизация", "Ссылка для отклика", "Почта для отклика", "Сайт компании", "HH", "Хабр", "Getmatch", "Telegram",
  "Жильё / ипотека — статус", "Жильё / ипотека — формат льготы", "Жильё / ипотека — условия", "Жильё / ипотека — что будет после увольнения", "Жильё / ипотека — источник",
  "Выручка — сумма", "Выручка — год", "Выручка — метрика", "Выручка — юрлицо", "Выручка — ИНН", "Выручка — состав суммы", "Выручка — охват показателя",
  "Выручка — статус подтверждения", "Выручка — источник 1", "Выручка — источник 2", "Выручка — комментарий",
  "VPN и AI — краткое резюме", "VPN", "AI", "ChatGPT", "Claude", "Grok", "Qwen", "DeepSeek", "Ограничения по данным",
  "VPN и AI — уровень подтверждения", "VPN и AI — источник 1", "VPN и AI — источник 2", "VPN и AI — дата проверки", "VPN и AI — вопрос рекрутеру", "Балл ИИ", "ИИ — обоснование и источники",
  "DreamJob", "Эйч", "rabota.ru"
];

export const COMPANY_REVIEW_IMPORT_EXPORT_HEADERS = [
  "ID компании",
  "Habr", "DreamJob", "GlassDoor", "SuperJob", "Правда Сотрудников", "Nahjob", "BossNadzor", "YandexMap",
  "Plus Short", "Minus Short", "Plus", "Minus"
];

export const COMPANY_OFFICE_IMPORT_EXPORT_HEADERS = [
  "ID компании", "Название компании", "Название / роль офиса", "Адрес", "Метро", "Основной офис", "Статус подтверждения", "Источник", "Широта", "Долгота"
];

export const AGENCY_IMPORT_EXPORT_HEADERS = [
  "ID агентства", "Место в рейтинге соискателей", "Название агентства",
  "Отрасль", "Тип агентства", "Функциональная специализация", "Уровень позиций", "Подходящие роли / направления",
  "Executive Search", "Закрытые вакансии", "Публичность вакансий", "Работодатель в вакансии", "Зарплата в вакансии",
  "Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до",
  "Сайт компании", "HH", "Хабр", "Getmatch", "Telegram", "Основной канал вакансий", "Качество публичных источников вакансий", "Актуальность вакансий",
  "Принимает CV в базу", "Рекомендуемый способ контакта", "Ссылка для отклика", "Почта для отклика", "Как работать с агентством",
  "Итоговая оценка", "Балл отзывов", "Балл релевантности", "Балл выручки",
  "Этика", "Честность вакансий", "Обратная связь", "Релевантность вакансий", "IT-экспертиза", "Знание клиента", "Организация процесса", "Поддержка кандидата",
  "Уверенность в данных", "Риски / что проверить",
  "Habr", "DreamJob", "GlassDoor", "SuperJob", "Правда Сотрудников", "Nahjob", "BossNadzor", "YandexMap", "2GIS", "Otzovik", "PersonAgency", "Yell",
  "Plus Short", "Minus Short", "Plus", "Minus",
  "Выручка — сумма", "Выручка — год", "Выручка — метрика", "Выручка — юрлицо", "Выручка — ИНН", "Выручка — состав суммы", "Выручка — охват показателя",
  "Выручка — статус подтверждения", "Выручка — источник 1", "Выручка — источник 2", "Выручка — комментарий"
];

export const HIRING_CONTACT_IMPORT_EXPORT_HEADERS = [
  "Тип организации", "ID организации", "Название организации", "Позиция контакта", "ФИО",
  "LinkedIn", "Facebook", "Сетка", "Telegram", "Телефон", "Email", "Специализация контакта"
];

export function companyImportInstruction(header, sheet) {
  if (sheet === "Контакты" && header === "Тип организации") return "Допустимые значения: Компания, Агентство.";
  if (sheet === "Контакты" && header === "ID организации") return "ID компании или агентства из соответствующего листа. Можно использовать вместе с названием для проверки.";
  if (sheet === "Контакты" && header === "Название организации") return "Название компании или агентства должно точно совпадать с соответствующим листом.";
  if (header === "Позиция контакта") return "Целое число от 1 до 3. Определяет порядок показа и уникальный слот контакта внутри организации.";
  if (header === "ФИО") return "Обязательное поле, не более 256 символов.";
  if (["LinkedIn", "Facebook", "Сетка"].includes(header)) return "Полный URL профиля, начинающийся с http:// или https://.";
  if (header === "Telegram") return "Ссылка на профиль или имя пользователя в формате @username.";
  if (header === "Телефон") return "Номер телефона в удобном для чтения формате, не более 64 символов.";
  if (header === "Email") return "Корректный email, не более 254 символов.";
  if (header === "Специализация контакта") return "Краткий текст о специализации консультанта, не более 512 символов.";
  if (header === "ID агентства") return "Постоянный целочисленный ID. При добавлении игнорируется; в режиме обновления используется для точного сопоставления.";
  if (header === "Название агентства") return "Обязательное поле. Укажите официальное или общепринятое название рекрутингового агентства с присутствием в Москве.";
  if (header === "Место в рейтинге соискателей") return "Обновляется backend при нажатии «Пересчитать рейтинг». Значение экспортируется для прозрачности.";
  if (header === "ID компании") {
    if (sheet === "Компании") return "Постоянный целочисленный ID. При добавлении новой компании игнорируется; в режиме обновления используется для точного сопоставления.";
    if (sheet === "Отзывы") return "Обязательный постоянный ID компании в базе данных. По нему строка отзывов связывается с компанией; название компании не используется.";
    return "ID компании из листа «Компании». Служит для удобства сверки; импорт офисов связывает строки по названию компании.";
  }
  if (header === "Название компании") return sheet === "Компании"
    ? "Обязательное поле. Укажите официальное или общепринятое название; используется для контроля дубликатов и сопоставления."
    : "Обязательное поле. Должно точно совпадать с названием на листе «Компании».";
  if (["Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до"].includes(header)) return "Целое число в рублях за месяц без символа валюты и разделителей. Граница «от» не должна превышать границу «до».";
  if (header === "Итоговая оценка") return sheet === "Агентства" ? "Рассчитывается backend по шкале 0–100 после нажатия «Пересчитать рейтинг»; импорт сам пересчёт не запускает." : "Рассчитывается автоматически по действующей формуле рейтинга; значение из файла используется только для сверки и не меняет расчёт.";
  if (AGENCY_RATING_INPUT_META.some(([, label]) => label === header)) return "Число от 0 до 10 с шагом 0,5. Пустое значение допустимо; для пересчёта нужны все восемь оценок.";
  if (["Балл отзывов", "Балл релевантности", "Балл выручки"].includes(header)) return "Расчётное поле. Экспортируется для прозрачности; импортированное значение не является источником истины и не сохраняется.";
  if (header === "Тип агентства") return `Допустимые значения: ${AGENCY_TYPE_OPTIONS.join("; ")}. Можно указать несколько через точку с запятой.`;
  if (header === "Функциональная специализация") return `Допустимые значения: ${AGENCY_FUNCTION_OPTIONS.join("; ")}. Можно указать несколько через точку с запятой.`;
  if (header === "Уровень позиций") return `Допустимые значения: ${AGENCY_POSITION_LEVEL_OPTIONS.join("; ")}. Можно указать несколько через точку с запятой.`;
  if (header === "Executive Search") return `Допустимые значения: ${AGENCY_EXECUTIVE_SEARCH_OPTIONS.join("; ")}.`;
  if (header === "Закрытые вакансии") return `Допустимые значения: ${AGENCY_CLOSED_VACANCIES_OPTIONS.join("; ")}.`;
  if (header === "Публичность вакансий") return `Допустимые значения: ${AGENCY_VACANCY_PUBLICITY_OPTIONS.join("; ")}.`;
  if (header === "Работодатель в вакансии") return `Допустимые значения: ${AGENCY_EMPLOYER_DISCLOSURE_OPTIONS.join("; ")}.`;
  if (header === "Зарплата в вакансии") return `Допустимые значения: ${AGENCY_SALARY_DISCLOSURE_OPTIONS.join("; ")}.`;
  if (header === "Основной канал вакансий") return `Допустимые значения: ${AGENCY_PRIMARY_CHANNEL_OPTIONS.join("; ")}.`;
  if (header === "Качество публичных источников вакансий") return `Допустимые значения: ${AGENCY_PUBLIC_SOURCE_QUALITY_OPTIONS.join("; ")}.`;
  if (header === "Актуальность вакансий") return `Допустимые значения: ${AGENCY_VACANCY_FRESHNESS_OPTIONS.join("; ")}.`;
  if (header === "Принимает CV в базу") return `Допустимые значения: ${AGENCY_ACCEPTS_CV_OPTIONS.join("; ")}.`;
  if (header === "Рекомендуемый способ контакта") return `Допустимые значения: ${AGENCY_CONTACT_METHOD_OPTIONS.join("; ")}. Можно указать несколько через точку с запятой.`;
  if (header === "Как работать с агентством") return "Практическая рекомендация соискателю, не более 512 символов.";
  if (header === "Балл ИИ") return "0 — нет подтверждений; 5 — профильные вакансии, пилоты или внедрение ИИ; 10 — значимое ИИ-направление, продукты, системное внедрение или специализированное оборудование. Для 5 и 10 обязательно обоснование с источниками. При добавлении пусто — ещё не оценено; при обновлении пустая или отсутствующая колонка сохраняет прежнее значение. Для снятия баллов укажите 0.";
  if (header === "ИИ — обоснование и источники") return "Текст до 10000 символов: факты, дата проверки и ссылки на проекты, внедрения, профильные вакансии или оборудование для ИИ. При обновлении пустое поле сохраняет прежнее обоснование. Доступ к ChatGPT сам по себе не является основанием для баллов.";
  if (header === "Балл WLB") return "Целое число от 0 до 100. В итоговом рейтинге показатель автоматически приводится к текущему весу WLB.";
  if (header === "Рейтинг сотрудников") return "Число от 0 до 5; десятичная часть допустима.";
  if (header === "Выручка — сумма") return "Неотрицательное число в рублях без сокращений и символа валюты.";
  if (header === "Выручка — год") return "Целый год в диапазоне 1900–2100.";
  if (header === "Выручка — ИНН") return "Один или несколько ИНН из 10 или 12 цифр, без пробелов. Несколько ИНН разделяйте точкой с запятой.";
  if (header === "VPN и AI — дата проверки") return "Дата в формате ГГГГ-ММ-ДД.";
  if (["Plus Short", "Minus Short"].includes(header)) return "Агрегированный текст отзывов с Markdown-форматированием, не более 512 символов.";
  if (["Plus", "Minus"].includes(header)) return "Агрегированный текст отзывов с Markdown-форматированием, не более 2000 символов.";
  if (header === "Почта для отклика") return "Один email или несколько адресов, разделённых точкой с запятой.";
  if (["Ссылка для отклика", "Сайт компании", "HH", "Хабр", "Getmatch", "Telegram", "DreamJob", "Эйч", "rabota.ru", "Жильё / ипотека — источник", "Выручка — источник 1", "Выручка — источник 2", "Источник", "Habr", "GlassDoor", "SuperJob", "Правда Сотрудников", "Nahjob", "BossNadzor", "YandexMap", "2GIS", "Otzovik", "PersonAgency", "Yell"].includes(header)) return "Полный URL, начинающийся с http:// или https://.";
  if (["Широта", "Долгота"].includes(header)) return header === "Широта" ? "Число от −90 до 90. Заполняется вместе с долготой." : "Число от −180 до 180. Заполняется вместе с широтой.";
  if (header === "Основной офис") return "Допустимые значения: Да, Нет. Для одной компании рекомендуется только один основной офис.";
  if (header === "Адрес") return "Обязательное поле для строки офиса. Укажите полный московский адрес.";
  if (header === "Статус подтверждения") return "Допустимые значения: Подтверждено, Частично подтверждено, Не подтверждено.";
  if (["IT-аккредитация / отсрочка", "Мобилизация", "VPN", "AI", "ChatGPT", "Claude", "Grok", "Qwen", "DeepSeek"].includes(header)) return "Рекомендуемые значения: Да, Нет, Не подтверждено. Допускайте уточняющий текст только при необходимости сохранить важный контекст.";
  if (header === "Уверенность в данных" || header === "VPN и AI — уровень подтверждения") return "Рекомендуемые значения: Высокая, Средняя, Низкая.";
  if (header === "Жильё / ипотека — статус") return "Рекомендуемые значения: Есть, Нет, Неизвестно.";
  return "Текстовое поле. Сохраняйте фактическую формулировку без сокращения существенных деталей; поле можно оставить пустым.";
}

export function companyImportExportValues(company, companyId) {
  const housing = company.housing || {}, revenue = company.revenue || {}, vpnAi = company.vpnAi || {}, sources = company.vacancySources || {};
  return [
    companyId, company.name, company.sector, company.roles, company.score ?? "", company.rating ?? "", company.wlb, company.wlbScore ?? "", company.confidence, company.risk,
    company.middle?.[0] ?? "", company.middle?.[1] ?? "", company.senior?.[0] ?? "", company.senior?.[1] ?? "", company.lead?.[0] ?? "", company.lead?.[1] ?? "", company.head?.[0] ?? "", company.head?.[1] ?? "",
    company.deferment, company.mobilization, company.applicationUrl, company.applicationEmail, sources.companySite, sources.hh, sources.habr, sources.getmatch, sources.telegram,
    housing.status, housing.form, housing.terms, housing.after, housing.source,
    revenue.amount ?? "", revenue.year ?? "", revenue.metric, revenue.legal, revenue.inn, revenue.composition, revenue.coverage, revenue.status, revenue.source1, revenue.source2, revenue.comment,
    vpnAi.summary, vpnAi.vpn, vpnAi.ai, vpnAi.chatgpt, vpnAi.claude, vpnAi.grok, vpnAi.qwen, vpnAi.deepseek, vpnAi.data, vpnAi.evidence, vpnAi.source1, vpnAi.source2, vpnAi.checked, vpnAi.recruiter, company.aiScore ?? "", company.aiEvidence || "",
    sources.dreamJob, sources.hCareers, sources.rabota
  ];
}

export function companyReviewImportExportValues(company, companyId) {
  const reviewSources = company.reviewSources || {}, reviews = company.reviews || {};
  return [
    companyId,
    reviewSources.habr, reviewSources.dreamJob, reviewSources.glassDoor, reviewSources.superJob, reviewSources.pravdaSotrudnikov, reviewSources.nahjob, reviewSources.bossNadzor, reviewSources.yandexMap,
    reviews.plusShort, reviews.minusShort, reviews.plus, reviews.minus
  ];
}

export function agencyImportExportValues(agency, agencyId) {
  const vacancySources = agency.vacancySources || {}, reviewSources = agency.reviewSources || {}, reviews = agency.reviews || {}, revenue = agency.revenue || {};
  return [
    agencyId, agency.rank ?? "", agency.name,
    agency.sector, (agency.agencyTypes || []).join("; "), (agency.functionalSpecializations || []).join("; "), (agency.positionLevels || []).join("; "), agency.roles,
    agency.executiveSearch, agency.closedVacancies, agency.vacancyPublicity, agency.employerDisclosure, agency.salaryDisclosure,
    agency.middle?.[0] ?? "", agency.middle?.[1] ?? "", agency.senior?.[0] ?? "", agency.senior?.[1] ?? "", agency.lead?.[0] ?? "", agency.lead?.[1] ?? "", agency.head?.[0] ?? "", agency.head?.[1] ?? "",
    vacancySources.companySite, vacancySources.hh, vacancySources.habr, vacancySources.getmatch, vacancySources.telegram, agency.primaryVacancyChannel, agency.publicSourcesQuality, agency.vacancyFreshness,
    agency.acceptsCv, (agency.recommendedContactMethods || []).join("; "), agency.applicationUrl, agency.applicationEmail, agency.workingGuide,
    agency.score ?? "", agency.reviewsScore ?? "", agency.relevanceScore ?? "", agency.revenueScore ?? "",
    agency.ethics ?? "", agency.vacancyHonesty ?? "", agency.feedback ?? "", agency.vacancyRelevance ?? "", agency.itExpertise ?? "", agency.clientKnowledge ?? "", agency.processOrganization ?? "", agency.candidateSupport ?? "",
    agency.confidence, agency.risk,
    reviewSources.habr, reviewSources.dreamJob, reviewSources.glassDoor, reviewSources.superJob, reviewSources.pravdaSotrudnikov, reviewSources.nahjob, reviewSources.bossNadzor, reviewSources.yandexMap, reviewSources.twoGis, reviewSources.otzovik, reviewSources.personAgency, reviewSources.yell,
    reviews.plusShort, reviews.minusShort, reviews.plus, reviews.minus,
    revenue.amount ?? "", revenue.year ?? "", revenue.metric, revenue.legal, revenue.inn, revenue.composition, revenue.coverage, revenue.status, revenue.source1, revenue.source2, revenue.comment
  ];
}

export function companyImportExportXlsx(data) {
  const numericCompanyColumns = new Set([0, 4, 5, 7, 10, 11, 12, 13, 14, 15, 16, 17, 32, 33, COMPANY_IMPORT_EXPORT_HEADERS.indexOf("Балл ИИ")]);
  const numericOfficeColumns = new Set([0, 8, 9]);
  const numericAgencyHeaders = new Set([
    "ID агентства", "Место в рейтинге соискателей", "Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до",
    "Итоговая оценка", "Балл отзывов", "Балл релевантности", "Балл выручки", ...AGENCY_RATING_INPUT_META.map(([, label]) => label), "Выручка — сумма", "Выручка — год"
  ]);
  const numericAgencyColumns = new Set(AGENCY_IMPORT_EXPORT_HEADERS.map((header, index) => numericAgencyHeaders.has(header) ? index : -1).filter((index) => index >= 0));
  const instructionRows = [
    ["Лист", "Поле", "Обязательность", "Требования к заполнению"],
    ["Компании", "Формула рейтинга v5", "Справочно", "Максимум 100: релевантность — 5, отсрочка — 15, отзывы — 20, WLB — 20, зарплата — 20, выручка — 10, ИИ — 10. Московский офис и прозрачность информации баллов не добавляют."],
    ["Компании", "Совместимость", "Справочно", "Старые файлы без колонок ИИ поддерживаются. Для новой компании отсутствие оценки даёт 0 баллов за ИИ; при обновлении пустые или отсутствующие поля ИИ сохраняют прежние значения."],
    ...COMPANY_IMPORT_EXPORT_HEADERS.map((header) => ["Компании", header, header === "Название компании" ? "Обязательно" : header === "ИИ — обоснование и источники" ? "Обязательно для 5 и 10 баллов ИИ" : "Необязательно", companyImportInstruction(header, "Компании")]),
    ...COMPANY_OFFICE_IMPORT_EXPORT_HEADERS.map((header) => ["Офисы", header, ["Название компании", "Адрес"].includes(header) ? "Обязательно" : "Необязательно", companyImportInstruction(header, "Офисы")]),
    ...COMPANY_REVIEW_IMPORT_EXPORT_HEADERS.map((header) => ["Отзывы", header, header === "ID компании" ? "Обязательно" : "Необязательно", companyImportInstruction(header, "Отзывы")]),
    ...AGENCY_IMPORT_EXPORT_HEADERS.map((header) => ["Агентства", header, header === "Название агентства" ? "Обязательно" : "Необязательно", companyImportInstruction(header, "Агентства")]),
    ...HIRING_CONTACT_IMPORT_EXPORT_HEADERS.map((header) => ["Контакты", header, ["Тип организации", "Позиция контакта", "ФИО"].includes(header) ? "Обязательно" : "Необязательно", companyImportInstruction(header, "Контакты")])
  ];
  const sheetDefinitions = [
    {
      name: "Компании",
      rows: [COMPANY_IMPORT_EXPORT_HEADERS, ...data.companies],
      numericColumns: numericCompanyColumns,
      columns: '<col min="1" max="1" width="14" customWidth="1"/><col min="2" max="4" width="28" customWidth="1"/><col min="5" max="18" width="16" customWidth="1"/><col min="19" max="59" width="34" customWidth="1"/>'
    },
    {
      name: "Офисы",
      rows: [COMPANY_OFFICE_IMPORT_EXPORT_HEADERS, ...data.offices],
      numericColumns: numericOfficeColumns,
      columns: '<col min="1" max="1" width="14" customWidth="1"/><col min="2" max="3" width="28" customWidth="1"/><col min="4" max="5" width="38" customWidth="1"/><col min="6" max="7" width="22" customWidth="1"/><col min="8" max="8" width="50" customWidth="1"/><col min="9" max="10" width="16" customWidth="1"/>'
    },
    {
      name: "Отзывы",
      rows: [COMPANY_REVIEW_IMPORT_EXPORT_HEADERS, ...data.reviews],
      numericColumns: new Set([0]),
      columns: '<col min="1" max="1" width="14" customWidth="1"/><col min="2" max="9" width="54" customWidth="1"/><col min="10" max="13" width="60" customWidth="1"/>'
    },
    {
      name: "Агентства",
      rows: [AGENCY_IMPORT_EXPORT_HEADERS, ...data.agencies],
      numericColumns: numericAgencyColumns,
      columns: '<col min="1" max="2" width="16" customWidth="1"/><col min="3" max="3" width="30" customWidth="1"/><col min="4" max="21" width="24" customWidth="1"/><col min="22" max="75" width="42" customWidth="1"/>'
    },
    {
      name: "Контакты",
      rows: [HIRING_CONTACT_IMPORT_EXPORT_HEADERS, ...data.contacts],
      numericColumns: new Set([1, 3]),
      columns: '<col min="1" max="1" width="18" customWidth="1"/><col min="2" max="2" width="16" customWidth="1"/><col min="3" max="5" width="30" customWidth="1"/><col min="6" max="7" width="48" customWidth="1"/><col min="8" max="11" width="32" customWidth="1"/>'
    },
    {
      name: "Инструкция",
      rows: instructionRows,
      numericColumns: new Set(),
      columns: '<col min="1" max="1" width="14" customWidth="1"/><col min="2" max="2" width="38" customWidth="1"/><col min="3" max="3" width="18" customWidth="1"/><col min="4" max="4" width="95" customWidth="1"/>'
    }
  ];
  const worksheets = sheetDefinitions.map((definition, sheetIndex) => {
    const rowXml = definition.rows.map((values, rowIndex) => `<row r="${rowIndex + 1}"${rowIndex === 0 ? ' ht="30" customHeight="1"' : ""}>${values.map((value, columnIndex) =>
      xlsxCell(`${xlsxColumnName(columnIndex + 1)}${rowIndex + 1}`, value, rowIndex === 0 ? 1 : 0, rowIndex > 0 && definition.numericColumns.has(columnIndex) && value !== "" && value != null)
    ).join("")}</row>`).join("");
    const lastColumn = xlsxColumnName(definition.rows[0].length);
    return [`xl/worksheets/sheet${sheetIndex + 1}.xml`, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:${lastColumn}${definition.rows.length}"/><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols>${definition.columns}</cols><sheetData>${rowXml}</sheetData><autoFilter ref="A1:${lastColumn}${definition.rows.length}"/></worksheet>`];
  });
  const sheetOverrides = sheetDefinitions.map((_, index) => `<Override PartName="/xl/worksheets/sheet${index + 1}.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`).join("");
  const workbookSheets = sheetDefinitions.map((definition, index) => `<sheet name="${xmlEscape(definition.name)}" sheetId="${index + 1}" r:id="rId${index + 1}"/>`).join("");
  const workbookRelationships = sheetDefinitions.map((_, index) => `<Relationship Id="rId${index + 1}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet${index + 1}.xml"/>`).join("");
  return zipStored([
    ["[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>${sheetOverrides}<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`],
    ["_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`],
    ["xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>${workbookSheets}</sheets></workbook>`],
    ["xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${workbookRelationships}<Relationship Id="rId${sheetDefinitions.length + 1}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`],
    ["xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F2937"/><bgColor rgb="FF1F2937"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs></styleSheet>`],
    ...worksheets
  ]);
}

export async function companiesImportExport(db) {
  await ensureCrmSchema(db);
  const catalog = await readAppCatalog(db);
  const [identities, agencyIdentities] = await Promise.all([ensureCompanyIdentities(db, catalog.companies), ensureAgencyIdentities(db, catalog.agencies)]);
  const orderedCompanies = catalog.companies.slice().sort((left, right) => Number(identities.get(left.name)) - Number(identities.get(right.name)));
  const companies = orderedCompanies.map((company) => companyImportExportValues(company, identities.get(company.name)));
  const reviews = orderedCompanies.map((company) => companyReviewImportExportValues(company, identities.get(company.name)));
  const offices = orderedCompanies.flatMap((company) => (Array.isArray(company.offices) ? company.offices : []).map((office) => [
    identities.get(company.name), company.name, office.role, office.address, office.metro, office.main, office.status, office.source, office.lat ?? "", office.lon ?? ""
  ]));
  const agencies = catalog.agencies.slice().sort(compareAgenciesForExport).map((agency) => agencyImportExportValues(agency, agencyIdentities.get(agency.name)));
  const companyNames = new Set(catalog.companies.map((company) => company.name));
  const agencyNames = new Set(catalog.agencies.map((agency) => agency.name));
  const contactResult = await db.prepare(
    `SELECT company_name AS companyName, position, full_name AS fullName, linkedin_url AS linkedinUrl,
            facebook_url AS facebookUrl, setka_url AS setkaUrl, telegram, phone, email, specialization
     FROM hiring_contacts
     ORDER BY company_name COLLATE NOCASE ASC, position ASC`
  ).all();
  const contacts = (contactResult?.results || []).filter((contact) => companyNames.has(contact.companyName) || agencyNames.has(contact.companyName)).map((contact) => {
    const isCompany = companyNames.has(contact.companyName);
    return [isCompany ? "Компания" : "Агентство", isCompany ? identities.get(contact.companyName) : agencyIdentities.get(contact.companyName), contact.companyName, contact.position, contact.fullName, contact.linkedinUrl, contact.facebookUrl, contact.setkaUrl, contact.telegram, contact.phone, contact.email, contact.specialization];
  });
  return { companies, offices, reviews, agencies, contacts };
}

export function compareAgenciesForExport(left, right) {
  return (Number(left.rank) || Number.MAX_SAFE_INTEGER) - (Number(right.rank) || Number.MAX_SAFE_INTEGER) || left.name.localeCompare(right.name, "ru");
}

export async function vacanciesExport(db, userName) {
  await ensureCrmSchema(db);
  const catalog = await readAppCatalog(db);
  await Promise.all([ensureCompanyIdentities(db, catalog.companies), ensureAgencyIdentities(db, catalog.agencies)]);
  const result = await db.prepare(
    `WITH RECURSIVE role_paths(id,path) AS (
       SELECT id,name FROM vacancy_roles WHERE parent_id IS NULL
       UNION ALL SELECT child.id,role_paths.path||' → '||child.name
       FROM vacancy_roles child JOIN role_paths ON child.parent_id=role_paths.id
     ) SELECT CASE WHEN vacancy.entity_type = 'company' THEN owner_identity.id END AS companyId,
            CASE WHEN vacancy.entity_type = 'company' THEN vacancy.company_name ELSE '' END AS companyName,
            vacancy.title,
            vacancy.url,
            vacancy.salary_from AS salaryFrom,
            vacancy.salary_to AS salaryTo,
            vacancy.level,
            COALESCE((SELECT group_concat(path,'; ') FROM (
              SELECT role_paths.path FROM vacancy_role_assignments assignment
              JOIN role_paths ON role_paths.id=assignment.role_id
              WHERE assignment.vacancy_id=vacancy.id ORDER BY role_paths.path
            )),vacancy.role) AS role,
            user_state.relevance,
            COALESCE(user_state.favorite, 0) AS favorite,
            COALESCE(user_state.hidden, 0) AS hidden,
            vacancy.published_at AS publishedAt,
            vacancy.loaded_at AS loadedAt,
            vacancy.archived,
            vacancy.description_md AS descriptionMd,
            vacancy.entity_type AS entityType,
            agency_identity.id AS agencyId,
            CASE WHEN vacancy.entity_type = 'agency' THEN vacancy.company_name ELSE '' END AS agencyName,
            real_identity.id AS realCompanyId,
            vacancy.real_company_name AS realCompanyName,
            vacancy.cover_letter_subject AS coverLetterSubject,
            vacancy.cover_letter_md AS coverLetterMd
     FROM company_vacancies AS vacancy
     LEFT JOIN company_vacancy_user_state AS user_state ON user_state.vacancy_id = vacancy.id AND user_state.user_name = ?
     LEFT JOIN company_identity AS owner_identity ON owner_identity.company_name = vacancy.company_name AND vacancy.entity_type = 'company'
     LEFT JOIN agency_identity ON agency_identity.agency_name = vacancy.company_name AND vacancy.entity_type = 'agency'
     LEFT JOIN company_identity AS real_identity ON real_identity.company_name = vacancy.real_company_name
     ORDER BY vacancy.entity_type ASC, COALESCE(owner_identity.id, agency_identity.id) ASC, vacancy.published_at IS NULL ASC, vacancy.published_at DESC, vacancy.id DESC`
  ).bind(userName).all();
  return result?.results || [];
}

export function vacanciesXlsx(rows) {
  const values = [VACANCY_EXPORT_HEADERS, ...rows.map((row) => [
    row.companyId,
    row.companyName,
    row.title,
    row.url,
    row.salaryFrom ?? "",
    row.salaryTo ?? "",
    row.level || "",
    row.role || "",
    row.relevance ?? "",
    row.publishedAt || "",
    row.loadedAt || "",
    row.descriptionMd || "",
    row.entityType === "agency" ? "Агентство" : "Компания",
    row.agencyId ?? "",
    row.agencyName || "",
    row.realCompanyId ?? "",
    row.realCompanyName || "",
    row.coverLetterSubject || "",
    row.coverLetterMd || ""
  ])];
  const numericColumns = new Set([0, 4, 5, 8, 13, 15]);
  const worksheetRows = values.map((row, rowIndex) =>
    `<row r="${rowIndex + 1}"${rowIndex === 0 ? ' ht="30" customHeight="1"' : ""}>${row.map((value, columnIndex) =>
      xlsxCell(
        `${xlsxColumnName(columnIndex + 1)}${rowIndex + 1}`,
        value,
        rowIndex === 0 ? 1 : 0,
        rowIndex > 0 && numericColumns.has(columnIndex) && value !== ""
      )
    ).join("")}</row>`
  ).join("");
  const sheet = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="1" width="14" customWidth="1"/><col min="2" max="3" width="30" customWidth="1"/><col min="4" max="4" width="58" customWidth="1"/><col min="5" max="6" width="14" customWidth="1"/><col min="7" max="7" width="20" customWidth="1"/><col min="8" max="8" width="28" customWidth="1"/><col min="9" max="9" width="16" customWidth="1"/><col min="10" max="11" width="17" customWidth="1"/><col min="12" max="12" width="90" customWidth="1"/><col min="13" max="17" width="28" customWidth="1"/></cols><sheetData>${worksheetRows}</sheetData><autoFilter ref="A1:Q${rows.length + 1}"/></worksheet>`;
  return zipStored([
    ["[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>`],
    ["_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`],
    ["xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Вакансии" sheetId="1" r:id="rId1"/></sheets></workbook>`],
    ["xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`],
    ["xl/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F2937"/><bgColor rgb="FF1F2937"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs></styleSheet>`],
    ["xl/worksheets/sheet1.xml", sheet]
  ]);
}
