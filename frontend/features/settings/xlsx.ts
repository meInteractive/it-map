type Cell = string | number | boolean;
type Entries = Map<string, Uint8Array<ArrayBuffer>>;
type FieldMap = Record<string, {
    label: string;
    aliases: string[];
    optional?: boolean;
}>;
function zipUint32(view: DataView, offset: number) { return view.getUint32(offset, true); }
function zipUint16(view: DataView, offset: number) { return view.getUint16(offset, true); }
async function unzipXlsx(arrayBuffer: ArrayBuffer) {
    const bytes = new Uint8Array(arrayBuffer), view = new DataView(arrayBuffer);
    let end = -1, minimum = Math.max(0, bytes.length - 65557);
    for (let offset = bytes.length - 22; offset >= minimum; offset--) {
        if (zipUint32(view, offset) === 0x06054b50) {
            end = offset;
            break;
        }
    }
    if (end < 0)
        throw new Error('Файл не является корректным Excel-документом.');
    const entries: Entries = new Map(), count = zipUint16(view, end + 10);
    let cursor = zipUint32(view, end + 16);
    for (let index = 0; index < count; index++) {
        if (zipUint32(view, cursor) !== 0x02014b50)
            throw new Error('Не удалось прочитать структуру Excel-файла.');
        const flags = zipUint16(view, cursor + 8), method = zipUint16(view, cursor + 10), compressedSize = zipUint32(view, cursor + 20), nameLength = zipUint16(view, cursor + 28), extraLength = zipUint16(view, cursor + 30), commentLength = zipUint16(view, cursor + 32), localOffset = zipUint32(view, cursor + 42);
        if (flags & 1)
            throw new Error('Excel-файл защищён паролем и не может быть импортирован.');
        const name = new TextDecoder().decode(bytes.slice(cursor + 46, cursor + 46 + nameLength));
        const localNameLength = zipUint16(view, localOffset + 26), localExtraLength = zipUint16(view, localOffset + 28), dataOffset = localOffset + 30 + localNameLength + localExtraLength;
        const compressed = bytes.slice(dataOffset, dataOffset + compressedSize);
        let content;
        if (method === 0)
            content = compressed;
        else if (method === 8) {
            try {
                const stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
                content = new Uint8Array(await new Response(stream).arrayBuffer());
            }
            catch {
                throw new Error('Браузер не смог распаковать Excel-файл. Попробуйте обновить браузер.');
            }
        }
        else
            throw new Error('В Excel-файле используется неподдерживаемый способ сжатия.');
        entries.set(name, content);
        cursor += 46 + nameLength + extraLength + commentLength;
    }
    return entries;
}
function xmlDocument(bytes: Uint8Array<ArrayBuffer> | undefined, label: string) {
    if (!bytes)
        throw new Error('В Excel-файле отсутствует ' + label + '.');
    const documentValue = new DOMParser().parseFromString(new TextDecoder().decode(bytes), 'application/xml');
    if (documentValue.querySelector('parsererror'))
        throw new Error('Не удалось прочитать ' + label + ' в Excel-файле.');
    return documentValue;
}
function xlsxSheetPaths(entries: Entries) {
    const workbook = xmlDocument(entries.get('xl/workbook.xml'), 'описание книги'), relationships = xmlDocument(entries.get('xl/_rels/workbook.xml.rels'), 'связи книги');
    const relationById = new Map([...relationships.getElementsByTagNameNS('*', 'Relationship')].map(item => [item.getAttribute('Id'), item]));
    const sheets = [...workbook.getElementsByTagNameNS('*', 'sheet')];
    if (!sheets.length)
        throw new Error('В Excel-файле нет листов.');
    return new Map(sheets.map(sheet => {
        const relationId = sheet.getAttributeNS('http://schemas.openxmlformats.org/officeDocument/2006/relationships', 'id') || sheet.getAttribute('r:id'), relation = relationById.get(relationId);
        if (!relation)
            throw new Error('Не удалось найти лист «' + String(sheet.getAttribute('name') || 'без названия') + '».');
        const target = String(relation.getAttribute('Target') || '').replace(/^\//, '');
        return [String(sheet.getAttribute('name') || ''), (target.startsWith('xl/') ? target : 'xl/' + target.replace(/^\.\//, ''))];
    }));
}
function xlsxFirstSheetPath(entries: Entries) {
    return xlsxSheetPaths(entries).values().next().value!;
}
function xlsxSharedStrings(entries: Entries) {
    const bytes = entries.get('xl/sharedStrings.xml');
    if (!bytes)
        return [];
    const documentValue = xmlDocument(bytes, 'таблицу строк');
    return [...documentValue.getElementsByTagNameNS('*', 'si')].map(item => [...item.getElementsByTagNameNS('*', 't')].map(textNode => textNode.textContent || '').join(''));
}
function xlsxColumnIndex(reference: string | null) {
    const letters = String(reference || '').match(/^[A-Z]+/i)?.[0]?.toUpperCase() || '';
    let value = 0;
    for (const letter of letters)
        value = value * 26 + letter.charCodeAt(0) - 64;
    return Math.max(0, value - 1);
}
function xlsxCellValue(cell: Element, sharedStrings: string[]) {
    const type = cell.getAttribute('t') || '', value = cell.getElementsByTagNameNS('*', 'v')[0]?.textContent || '';
    if (type === 'inlineStr')
        return [...cell.getElementsByTagNameNS('*', 't')].map(item => item.textContent || '').join('');
    if (type === 's')
        return sharedStrings[Number(value)] ?? '';
    if (type === 'b')
        return value === '1';
    if (type === 'str' || type === 'e')
        return value;
    return value === '' ? '' : Number.isFinite(Number(value)) ? Number(value) : value;
}
function normalizeImportHeader(value: unknown) { return String(value ?? '').trim().toLowerCase().replaceAll('ё', 'е').replace(/[^a-zа-я0-9]+/g, ''); }
function excelDateValue(value: Cell | undefined) {
    if (value == null || value === '')
        return '';
    if (typeof value === 'number' && Number.isFinite(value)) {
        const date = new Date(Math.round((value - 25569) * 86400000));
        return Number.isNaN(date.getTime()) ? '' : date.toISOString().slice(0, 10);
    }
    const text = String(value).trim(), match = text.match(/^(\d{1,2})[.\/-](\d{1,2})[.\/-](\d{4})$/);
    if (match)
        return match[3] + '-' + match[2].padStart(2, '0') + '-' + match[1].padStart(2, '0');
    const parsed = new Date(text);
    return Number.isNaN(parsed.getTime()) ? text : parsed.toISOString().slice(0, 10);
}
function excelIntegerValue(value: Cell | undefined) {
    if (value == null || value === '')
        return null;
    if (typeof value === 'number')
        return Number.isFinite(value) ? Math.round(value) : null;
    const normalized = String(value).replace(/[^\d.-]/g, '');
    return normalized && Number.isFinite(Number(normalized)) ? Math.round(Number(normalized)) : value;
}
function xlsxSheetRows(entries: Entries, path: string, label: string, sharedStrings: string[]) {
    const sheet = xmlDocument(entries.get(path), 'лист «' + label + '»');
    return [...sheet.getElementsByTagNameNS('*', 'row')].map(row => {
        const values: Cell[] = [];
        for (const cell of row.getElementsByTagNameNS('*', 'c'))
            values[xlsxColumnIndex(cell.getAttribute('r'))] = xlsxCellValue(cell, sharedStrings);
        return values;
    }).filter(row => row.some(value => String(value ?? '').trim() !== ''));
}
function importFieldIndexes(headerRow: Cell[], fields: FieldMap, label: string) {
    const headerMap = new Map(headerRow.map((value, index) => [normalizeImportHeader(value), index])), indexes: Record<string, number> = {};
    for (const [field, config] of Object.entries(fields)) {
        indexes[field] = config.aliases.map(alias => headerMap.get(normalizeImportHeader(alias))).find(index => index !== undefined) ?? -1;
        if (indexes[field] < 0) {
            if (config.optional) {
                indexes[field] = -1;
                continue;
            }
            throw new Error('На листе «' + label + '» не найден столбец «' + config.label + '».');
        }
    }
    return indexes;
}
export async function parseCompanyWorkbook(file: File) {
    if (file.size > 25 * 1024 * 1024)
        throw new Error('Excel-файл больше допустимых 25 МБ.');
    const entries = await unzipXlsx(await file.arrayBuffer()), sharedStrings = xlsxSharedStrings(entries), sheetPaths = xlsxSheetPaths(entries);
    const companyPath = sheetPaths.get('Компании'), officePath = sheetPaths.get('Офисы'), reviewPath = sheetPaths.get('Отзывы'), agencyPath = sheetPaths.get('Агентства'), contactPath = sheetPaths.get('Контакты');
    if (!companyPath)
        throw new Error('В Excel-файле отсутствует лист «Компании».');
    if (!officePath)
        throw new Error('В Excel-файле отсутствует лист «Офисы».');
    const companyRows = xlsxSheetRows(entries, companyPath, 'Компании', sharedStrings), officeRows = xlsxSheetRows(entries, officePath, 'Офисы', sharedStrings), reviewRows = reviewPath ? xlsxSheetRows(entries, reviewPath, 'Отзывы', sharedStrings) : [], agencyRows = agencyPath ? xlsxSheetRows(entries, agencyPath, 'Агентства', sharedStrings) : [], contactRows = contactPath ? xlsxSheetRows(entries, contactPath, 'Контакты', sharedStrings) : [];
    if (companyRows.length < 2)
        throw new Error('На листе «Компании» нет строк для импорта.');
    if (!officeRows.length)
        throw new Error('На листе «Офисы» отсутствует строка заголовков.');
    const companyFields: FieldMap = {
        aiScore: { label: 'Балл ИИ', aliases: ['Балл ИИ'], optional: true }, aiEvidence: { label: 'ИИ — обоснование и источники', aliases: ['ИИ — обоснование и источники'], optional: true },
        companyId: { label: 'ID компании', aliases: ['ID компании', 'ID компании в БД'] }, name: { label: 'Название компании', aliases: ['Название компании'] }, sector: { label: 'Отрасль', aliases: ['Отрасль'] }, roles: { label: 'Подходящие роли / направления', aliases: ['Подходящие роли / направления'] }, score: { label: 'Итоговая оценка', aliases: ['Итоговая оценка'] }, rating: { label: 'Рейтинг сотрудников', aliases: ['Рейтинг сотрудников'] }, wlb: { label: 'Work-life balance', aliases: ['Work-life balance'] }, wlbScore: { label: 'Балл WLB', aliases: ['Балл WLB'] }, confidence: { label: 'Уверенность в данных', aliases: ['Уверенность в данных'] }, risk: { label: 'Риски / что проверить', aliases: ['Риски / что проверить'] }, middleFrom: { label: 'Middle — от', aliases: ['Middle — от'] }, middleTo: { label: 'Middle — до', aliases: ['Middle — до'] }, seniorFrom: { label: 'Senior — от', aliases: ['Senior — от'] }, seniorTo: { label: 'Senior — до', aliases: ['Senior — до'] }, leadFrom: { label: 'Lead — от', aliases: ['Lead — от'], optional: true }, leadTo: { label: 'Lead — до', aliases: ['Lead — до'], optional: true }, headFrom: { label: 'Head — от', aliases: ['Head — от'], optional: true }, headTo: { label: 'Head — до', aliases: ['Head — до'], optional: true }, deferment: { label: 'IT-аккредитация / отсрочка', aliases: ['IT-аккредитация / отсрочка', 'Отсрочка'] }, mobilization: { label: 'Мобилизация', aliases: ['Мобилизация', 'Бронь от мобилизации', 'Бронь'] }, applicationUrl: { label: 'Ссылка для отклика', aliases: ['Ссылка для отклика'] }, applicationEmail: { label: 'Почта для отклика', aliases: ['Почта для отклика'] }, companySite: { label: 'Сайт компании', aliases: ['Сайт компании'] }, hh: { label: 'HH', aliases: ['HH'] }, habr: { label: 'Хабр', aliases: ['Хабр'] }, getmatch: { label: 'Getmatch', aliases: ['Getmatch'] }, telegram: { label: 'Telegram', aliases: ['Telegram'] }, vacancyDreamJob: { label: 'DreamJob', aliases: ['DreamJob'], optional: true }, hCareers: { label: 'Эйч', aliases: ['Эйч', 'H Careers', 'h.careers'], optional: true }, rabota: { label: 'rabota.ru', aliases: ['rabota.ru', 'Работа.ру'], optional: true }, housingStatus: { label: 'Жильё / ипотека — статус', aliases: ['Жильё / ипотека — статус'] }, housingForm: { label: 'Жильё / ипотека — формат льготы', aliases: ['Жильё / ипотека — формат льготы'] }, housingTerms: { label: 'Жильё / ипотека — условия', aliases: ['Жильё / ипотека — условия'] }, housingAfter: { label: 'Жильё / ипотека — что будет после увольнения', aliases: ['Жильё / ипотека — что будет после увольнения'] }, housingSource: { label: 'Жильё / ипотека — источник', aliases: ['Жильё / ипотека — источник'] }, revenueAmount: { label: 'Выручка — сумма', aliases: ['Выручка — сумма'] }, revenueYear: { label: 'Выручка — год', aliases: ['Выручка — год'] }, revenueMetric: { label: 'Выручка — метрика', aliases: ['Выручка — метрика'] }, revenueLegal: { label: 'Выручка — юрлицо', aliases: ['Выручка — юрлицо'] }, revenueInn: { label: 'Выручка — ИНН', aliases: ['Выручка — ИНН'] }, revenueComposition: { label: 'Выручка — состав суммы', aliases: ['Выручка — состав суммы'] }, revenueCoverage: { label: 'Выручка — охват показателя', aliases: ['Выручка — охват показателя'] }, revenueStatus: { label: 'Выручка — статус подтверждения', aliases: ['Выручка — статус подтверждения'] }, revenueSource1: { label: 'Выручка — источник 1', aliases: ['Выручка — источник 1'] }, revenueSource2: { label: 'Выручка — источник 2', aliases: ['Выручка — источник 2'] }, revenueComment: { label: 'Выручка — комментарий', aliases: ['Выручка — комментарий'] }, vpnAiSummary: { label: 'VPN и AI — краткое резюме', aliases: ['VPN и AI — краткое резюме'] }, vpn: { label: 'VPN', aliases: ['VPN'] }, ai: { label: 'AI', aliases: ['AI'] }, chatgpt: { label: 'ChatGPT', aliases: ['ChatGPT'] }, claude: { label: 'Claude', aliases: ['Claude'] }, grok: { label: 'Grok', aliases: ['Grok'] }, qwen: { label: 'Qwen', aliases: ['Qwen'] }, deepseek: { label: 'DeepSeek', aliases: ['DeepSeek'] }, vpnAiData: { label: 'Ограничения по данным', aliases: ['Ограничения по данным'] }, vpnAiEvidence: { label: 'VPN и AI — уровень подтверждения', aliases: ['VPN и AI — уровень подтверждения'] }, vpnAiSource1: { label: 'VPN и AI — источник 1', aliases: ['VPN и AI — источник 1'] }, vpnAiSource2: { label: 'VPN и AI — источник 2', aliases: ['VPN и AI — источник 2'] }, vpnAiChecked: { label: 'VPN и AI — дата проверки', aliases: ['VPN и AI — дата проверки'] }, vpnAiRecruiter: { label: 'VPN и AI — вопрос рекрутеру', aliases: ['VPN и AI — вопрос рекрутеру'] }, reviewHabr: { label: 'Habr', aliases: ['Habr'], optional: true }, reviewDreamJob: { label: 'DreamJob', aliases: ['DreamJob'], optional: true }, reviewGlassDoor: { label: 'GlassDoor', aliases: ['GlassDoor', 'Glassdoor'], optional: true }, reviewSuperJob: { label: 'SuperJob', aliases: ['SuperJob'], optional: true }, reviewPravdaSotrudnikov: { label: 'Правда Сотрудников', aliases: ['Правда Сотрудников'], optional: true }, reviewNahjob: { label: 'Nahjob', aliases: ['Nahjob'], optional: true }, reviewBossNadzor: { label: 'BossNadzor', aliases: ['BossNadzor'], optional: true }, reviewYandexMap: { label: 'YandexMap', aliases: ['YandexMap', 'Yandex Maps'], optional: true }, reviewsPlusShort: { label: 'Plus Short', aliases: ['Plus Short'], optional: true }, reviewsMinusShort: { label: 'Minus Short', aliases: ['Minus Short'], optional: true }, reviewsPlus: { label: 'Plus', aliases: ['Plus'], optional: true }, reviewsMinus: { label: 'Minus', aliases: ['Minus'], optional: true }
    };
    const normalizedCompanyHeaders = companyRows[0].map(normalizeImportHeader);
    const dreamJobHeaderIndexes = normalizedCompanyHeaders.map((header, index) => header === normalizeImportHeader('DreamJob') ? index : -1).filter(index => index >= 0);
    const dreamJobHeaderIndex = dreamJobHeaderIndexes.at(-1) ?? -1;
    const aiEvidenceHeaderIndex = normalizedCompanyHeaders.indexOf(normalizeImportHeader('ИИ — обоснование и источники'));
    const hCareersHeaderIndex = normalizedCompanyHeaders.indexOf(normalizeImportHeader('Эйч'));
    const dreamJobIsVacancySource = hCareersHeaderIndex >= 0 || (dreamJobHeaderIndex >= 0 && aiEvidenceHeaderIndex >= 0 && dreamJobHeaderIndex > aiEvidenceHeaderIndex);
    if (dreamJobIsVacancySource)
        companyFields.reviewDreamJob.aliases = ['DreamJob — отзывы', 'DreamJob (отзывы)'];
    else
        companyFields.vacancyDreamJob.aliases = ['DreamJob — вакансии'];
    const officeFields: FieldMap = { companyId: { label: 'ID компании', aliases: ['ID компании'] }, companyName: { label: 'Название компании', aliases: ['Название компании'] }, role: { label: 'Название / роль офиса', aliases: ['Название / роль офиса'] }, address: { label: 'Адрес', aliases: ['Адрес'] }, metro: { label: 'Метро', aliases: ['Метро'] }, main: { label: 'Основной офис', aliases: ['Основной офис'] }, status: { label: 'Статус подтверждения', aliases: ['Статус подтверждения'] }, source: { label: 'Источник', aliases: ['Источник'] }, lat: { label: 'Широта', aliases: ['Широта'] }, lon: { label: 'Долгота', aliases: ['Долгота'] } };
    const reviewFields: FieldMap = { companyId: { label: 'ID компании', aliases: ['ID компании', 'ID компании в БД'] }, reviewHabr: { label: 'Habr', aliases: ['Habr'] }, reviewDreamJob: { label: 'DreamJob', aliases: ['DreamJob'] }, reviewGlassDoor: { label: 'GlassDoor', aliases: ['GlassDoor', 'Glassdoor'] }, reviewSuperJob: { label: 'SuperJob', aliases: ['SuperJob'] }, reviewPravdaSotrudnikov: { label: 'Правда Сотрудников', aliases: ['Правда Сотрудников'] }, reviewNahjob: { label: 'Nahjob', aliases: ['Nahjob'] }, reviewBossNadzor: { label: 'BossNadzor', aliases: ['BossNadzor'] }, reviewYandexMap: { label: 'YandexMap', aliases: ['YandexMap', 'Yandex Maps'] }, reviewsPlusShort: { label: 'Plus Short', aliases: ['Plus Short'] }, reviewsMinusShort: { label: 'Minus Short', aliases: ['Minus Short'] }, reviewsPlus: { label: 'Plus', aliases: ['Plus'] }, reviewsMinus: { label: 'Minus', aliases: ['Minus'] } };
    const agencyFields: FieldMap = {
        agencyId: { label: 'ID агентства', aliases: ['ID агентства'], optional: true }, rank: { label: 'Место в рейтинге соискателей', aliases: ['Место в рейтинге соискателей', 'Место в рейтинге'], optional: true }, name: { label: 'Название агентства', aliases: ['Название агентства'], optional: true },
        sector: { label: 'Отрасль', aliases: ['Отрасль'], optional: true }, agencyTypes: { label: 'Тип агентства', aliases: ['Тип агентства'], optional: true }, functionalSpecializations: { label: 'Функциональная специализация', aliases: ['Функциональная специализация'], optional: true }, positionLevels: { label: 'Уровень позиций', aliases: ['Уровень позиций'], optional: true }, roles: { label: 'Подходящие роли / направления', aliases: ['Подходящие роли / направления'], optional: true },
        executiveSearch: { label: 'Executive Search', aliases: ['Executive Search'], optional: true }, closedVacancies: { label: 'Закрытые вакансии', aliases: ['Закрытые вакансии'], optional: true }, vacancyPublicity: { label: 'Публичность вакансий', aliases: ['Публичность вакансий'], optional: true }, employerDisclosure: { label: 'Работодатель в вакансии', aliases: ['Работодатель в вакансии'], optional: true }, salaryDisclosure: { label: 'Зарплата в вакансии', aliases: ['Зарплата в вакансии'], optional: true },
        middleFrom: { label: 'Middle — от', aliases: ['Middle — от'], optional: true }, middleTo: { label: 'Middle — до', aliases: ['Middle — до'], optional: true }, seniorFrom: { label: 'Senior — от', aliases: ['Senior — от'], optional: true }, seniorTo: { label: 'Senior — до', aliases: ['Senior — до'], optional: true }, leadFrom: { label: 'Lead — от', aliases: ['Lead — от'], optional: true }, leadTo: { label: 'Lead — до', aliases: ['Lead — до'], optional: true }, headFrom: { label: 'Head — от', aliases: ['Head — от'], optional: true }, headTo: { label: 'Head — до', aliases: ['Head — до'], optional: true },
        companySite: { label: 'Сайт компании', aliases: ['Сайт компании'], optional: true }, hh: { label: 'HH', aliases: ['HH'], optional: true }, habr: { label: 'Хабр', aliases: ['Хабр'], optional: true }, getmatch: { label: 'Getmatch', aliases: ['Getmatch'], optional: true }, telegram: { label: 'Telegram', aliases: ['Telegram'], optional: true }, primaryVacancyChannel: { label: 'Основной канал вакансий', aliases: ['Основной канал вакансий'], optional: true }, publicSourcesQuality: { label: 'Качество публичных источников вакансий', aliases: ['Качество публичных источников вакансий'], optional: true }, vacancyFreshness: { label: 'Актуальность вакансий', aliases: ['Актуальность вакансий'], optional: true },
        acceptsCv: { label: 'Принимает CV в базу', aliases: ['Принимает CV в базу'], optional: true }, recommendedContactMethods: { label: 'Рекомендуемый способ контакта', aliases: ['Рекомендуемый способ контакта'], optional: true }, applicationUrl: { label: 'Ссылка для отклика', aliases: ['Ссылка для отклика'], optional: true }, applicationEmail: { label: 'Почта для отклика', aliases: ['Почта для отклика'], optional: true }, workingGuide: { label: 'Как работать с агентством', aliases: ['Как работать с агентством'], optional: true },
        score: { label: 'Итоговая оценка', aliases: ['Итоговая оценка'], optional: true }, reviewsScore: { label: 'Балл отзывов', aliases: ['Балл отзывов'], optional: true }, relevanceScore: { label: 'Балл релевантности', aliases: ['Балл релевантности'], optional: true }, revenueScore: { label: 'Балл выручки', aliases: ['Балл выручки'], optional: true },
        ethics: { label: 'Этика', aliases: ['Этика'], optional: true }, vacancyHonesty: { label: 'Честность вакансий', aliases: ['Честность вакансий'], optional: true }, feedback: { label: 'Обратная связь', aliases: ['Обратная связь'], optional: true }, vacancyRelevance: { label: 'Релевантность вакансий', aliases: ['Релевантность вакансий'], optional: true }, itExpertise: { label: 'IT-экспертиза', aliases: ['IT-экспертиза', 'IT экспертиза'], optional: true }, clientKnowledge: { label: 'Знание клиента', aliases: ['Знание клиента'], optional: true }, processOrganization: { label: 'Организация процесса', aliases: ['Организация процесса'], optional: true }, candidateSupport: { label: 'Поддержка кандидата', aliases: ['Поддержка кандидата'], optional: true },
        confidence: { label: 'Уверенность в данных', aliases: ['Уверенность в данных'], optional: true }, risk: { label: 'Риски / что проверить', aliases: ['Риски / что проверить'], optional: true },
        reviewHabr: { label: 'Habr', aliases: ['Habr'], optional: true }, reviewDreamJob: { label: 'DreamJob', aliases: ['DreamJob'], optional: true }, reviewGlassDoor: { label: 'GlassDoor', aliases: ['GlassDoor'], optional: true }, reviewSuperJob: { label: 'SuperJob', aliases: ['SuperJob'], optional: true }, reviewPravdaSotrudnikov: { label: 'Правда Сотрудников', aliases: ['Правда Сотрудников'], optional: true }, reviewNahjob: { label: 'Nahjob', aliases: ['Nahjob'], optional: true }, reviewBossNadzor: { label: 'BossNadzor', aliases: ['BossNadzor'], optional: true }, reviewYandexMap: { label: 'YandexMap', aliases: ['YandexMap'], optional: true }, reviewTwoGis: { label: '2GIS', aliases: ['2GIS', '2ГИС'], optional: true }, reviewOtzovik: { label: 'Otzovik', aliases: ['Otzovik', 'Отзовик'], optional: true }, reviewPersonAgency: { label: 'PersonAgency', aliases: ['PersonAgency', 'Person Agency'], optional: true }, reviewYell: { label: 'Yell', aliases: ['Yell'], optional: true }, reviewsPlusShort: { label: 'Plus Short', aliases: ['Plus Short'], optional: true }, reviewsMinusShort: { label: 'Minus Short', aliases: ['Minus Short'], optional: true }, reviewsPlus: { label: 'Plus', aliases: ['Plus'], optional: true }, reviewsMinus: { label: 'Minus', aliases: ['Minus'], optional: true },
        revenueAmount: { label: 'Выручка — сумма', aliases: ['Выручка — сумма'], optional: true }, revenueYear: { label: 'Выручка — год', aliases: ['Выручка — год'], optional: true }, revenueMetric: { label: 'Выручка — метрика', aliases: ['Выручка — метрика'], optional: true }, revenueLegal: { label: 'Выручка — юрлицо', aliases: ['Выручка — юрлицо'], optional: true }, revenueInn: { label: 'Выручка — ИНН', aliases: ['Выручка — ИНН'], optional: true }, revenueComposition: { label: 'Выручка — состав суммы', aliases: ['Выручка — состав суммы'], optional: true }, revenueCoverage: { label: 'Выручка — охват показателя', aliases: ['Выручка — охват показателя'], optional: true }, revenueStatus: { label: 'Выручка — статус подтверждения', aliases: ['Выручка — статус подтверждения'], optional: true }, revenueSource1: { label: 'Выручка — источник 1', aliases: ['Выручка — источник 1', 'Выручка — источник'], optional: true }, revenueSource2: { label: 'Выручка — источник 2', aliases: ['Выручка — источник 2'], optional: true }, revenueComment: { label: 'Выручка — комментарий', aliases: ['Выручка — комментарий'], optional: true }
    };
    const contactFields: FieldMap = { entityType: { label: 'Тип организации', aliases: ['Тип организации'] }, entityId: { label: 'ID организации', aliases: ['ID организации'], optional: true }, entityName: { label: 'Название организации', aliases: ['Название организации'], optional: true }, position: { label: 'Позиция контакта', aliases: ['Позиция контакта', 'Номер контакта'] }, fullName: { label: 'ФИО', aliases: ['ФИО'] }, linkedinUrl: { label: 'LinkedIn', aliases: ['LinkedIn', 'Linkedin'], optional: true }, facebookUrl: { label: 'Facebook', aliases: ['Facebook'], optional: true }, setkaUrl: { label: 'Сетка', aliases: ['Сетка', 'Setka'], optional: true }, telegram: { label: 'Telegram', aliases: ['Telegram'], optional: true }, phone: { label: 'Телефон', aliases: ['Телефон', 'Номер телефона'], optional: true }, email: { label: 'Email', aliases: ['Email', 'E-mail'], optional: true }, specialization: { label: 'Специализация контакта', aliases: ['Специализация контакта'], optional: true } };
    if (reviewPath && !reviewRows.length)
        throw new Error('На листе «Отзывы» отсутствует строка заголовков.');
    if (contactPath && !contactRows.length)
        throw new Error('На листе «Контакты» отсутствует строка заголовков.');
    const companyIndexes = importFieldIndexes(companyRows[0], companyFields, 'Компании'), officeIndexes = importFieldIndexes(officeRows[0], officeFields, 'Офисы'), reviewIndexes = reviewPath ? importFieldIndexes(reviewRows[0], reviewFields, 'Отзывы') : {}, agencyIndexes = agencyPath && agencyRows.length ? importFieldIndexes(agencyRows[0], agencyFields, 'Агентства') : null, contactIndexes = contactPath && contactRows.length ? importFieldIndexes(contactRows[0], contactFields, 'Контакты') : null;
    if (dreamJobIsVacancySource) {
        companyIndexes.vacancyDreamJob = dreamJobHeaderIndex;
        if (dreamJobHeaderIndexes.length > 1)
            companyIndexes.reviewDreamJob = dreamJobHeaderIndexes[0];
    }
    const companies = companyRows.slice(1).map((row, index) => ({ row: index + 2, ...Object.fromEntries(Object.keys(companyIndexes).map(field => [field, companyIndexes[field] < 0 ? '' : row[companyIndexes[field]] ?? ''])) })).filter(item => Object.entries(item).some(([key, value]) => key !== 'row' && String(value ?? '').trim() !== ''));
    const offices = officeRows.slice(1).map((row, index) => ({ row: index + 2, ...Object.fromEntries(Object.keys(officeIndexes).map(field => [field, row[officeIndexes[field]] ?? ''])) })).filter(item => Object.entries(item).some(([key, value]) => key !== 'row' && String(value ?? '').trim() !== ''));
    const reviews = reviewPath ? reviewRows.slice(1).map((row, index) => ({ row: index + 2, ...Object.fromEntries(Object.keys(reviewIndexes).map(field => [field, row[reviewIndexes[field]] ?? ''])) })).filter(item => Object.entries(item).some(([key, value]) => !['row', 'companyId'].includes(key) && String(value ?? '').trim() !== '')) : [];
    const agencyPresentFields = agencyIndexes ? Object.keys(agencyIndexes).filter(field => agencyIndexes[field] >= 0) : [];
    const agencies = agencyIndexes ? agencyRows.slice(1).map((row, index) => ({ row: index + 2, __presentFields: agencyPresentFields, ...Object.fromEntries(Object.keys(agencyIndexes).map(field => [field, agencyIndexes[field] < 0 ? '' : row[agencyIndexes[field]] ?? ''])) })).filter(item => Object.entries(item).some(([key, value]) => !['row', '__presentFields'].includes(key) && String(value ?? '').trim() !== '')) : [];
    const contacts = contactIndexes ? contactRows.slice(1).map((row, index) => ({ row: index + 2, ...Object.fromEntries(Object.keys(contactIndexes).map(field => [field, contactIndexes[field] < 0 ? '' : row[contactIndexes[field]] ?? ''])) })).filter(item => Object.entries(item).some(([key, value]) => key !== 'row' && String(value ?? '').trim() !== '')) : [];
    if (companies.length > 500)
        throw new Error('За один раз можно импортировать не более 500 компаний.');
    if (offices.length > 2000)
        throw new Error('За один раз можно импортировать не более 2000 офисов.');
    if (reviews.length > 500)
        throw new Error('За один раз можно импортировать не более 500 строк отзывов.');
    if (agencies.length > 500)
        throw new Error('За один раз можно импортировать не более 500 агентств.');
    if (contacts.length > 1500)
        throw new Error('За один раз можно импортировать не более 1500 контактов.');
    return { companies, offices, reviews, agencies, contacts };
}
export async function parseVacancyWorkbook(file: File) {
    if (file.size > 25 * 1024 * 1024)
        throw new Error('Excel-файл больше допустимых 25 МБ.');
    const entries = await unzipXlsx(await file.arrayBuffer()), sharedStrings = xlsxSharedStrings(entries), sheet = xmlDocument(entries.get(xlsxFirstSheetPath(entries)), 'первый лист');
    const sourceRows = [...sheet.getElementsByTagNameNS('*', 'row')].map(row => {
        const values: Cell[] = [];
        for (const cell of row.getElementsByTagNameNS('*', 'c'))
            values[xlsxColumnIndex(cell.getAttribute('r'))] = xlsxCellValue(cell, sharedStrings);
        return values;
    }).filter(row => row.some(value => String(value ?? '').trim() !== ''));
    if (sourceRows.length < 2)
        throw new Error('В Excel-файле нет строк с вакансиями.');
    const headerMap = new Map(sourceRows[0].map((value, index) => [normalizeImportHeader(value), index]));
    const fields = { companyId: ['idкомпании', 'idкомпаниивбд'], companyName: ['названиекомпании'], title: ['названиевакансии'], role: ['роль'], salaryFrom: ['зарплатаот'], salaryTo: ['зарплатадо'], level: ['уровеньвакансии'], relevance: ['релевантность'], publishedAt: ['датапубликации'], loadedAt: ['датазагрузки'], descriptionMd: ['текствакансииmarkdown', 'полныйтекствакансиисформатированиемвформатеmarkdown', 'полныйтекствакансииmarkdown', 'полныйтекствакансии'], url: ['ссылканавакансию'] };
    const indexes: Record<string, number> = {};
    for (const [field, aliases] of Object.entries(fields)) {
        indexes[field] = aliases.map(alias => headerMap.get(alias)).find(index => index !== undefined) ?? -1;
        if (indexes[field] < 0)
            throw new Error('Не найден столбец «' + ({ companyId: 'ID компании', companyName: 'Название компании', title: 'Название вакансии', role: 'Роль', salaryFrom: 'Зарплата от', salaryTo: 'Зарплата до', level: 'Уровень вакансии', relevance: 'Релевантность', publishedAt: 'Дата публикации', loadedAt: 'Дата загрузки', descriptionMd: 'Текст вакансии (Markdown)', url: 'Ссылка на вакансию' }[field]) + '».');
    }
    const optionalFields = { entityType: ['типвладельца'], agencyId: ['идагентства'], agencyName: ['названиеагентства'], realCompanyId: ['идреальногоработодателя'], realCompanyName: ['реальныйработодатель', 'названиереальногоработодателя'], coverLetterSubject: ['заголовоксопроводительногописьма', 'темасопроводительногописьма'], coverLetterMd: ['текстсопроводительногописьмаmarkdown', 'сопроводительноеписьмоmarkdown', 'текстсопроводительногописьма'] };
    for (const [field, aliases] of Object.entries(optionalFields))
        indexes[field] = aliases.map(alias => headerMap.get(alias)).find(index => index !== undefined) ?? -1;
    const optional = (row: Cell[], field: string) => indexes[field] < 0 ? '' : row[indexes[field]] ?? '';
    const rows = sourceRows.slice(1).map(row => ({ companyId: excelIntegerValue(row[indexes.companyId]), companyName: String(row[indexes.companyName] ?? ''), entityType: String(optional(row, 'entityType')), agencyId: excelIntegerValue(optional(row, 'agencyId')), agencyName: String(optional(row, 'agencyName')), realCompanyId: excelIntegerValue(optional(row, 'realCompanyId')), realCompanyName: String(optional(row, 'realCompanyName')), title: String(row[indexes.title] ?? ''), role: String(row[indexes.role] ?? ''), salaryFrom: excelIntegerValue(row[indexes.salaryFrom]), salaryTo: excelIntegerValue(row[indexes.salaryTo]), level: String(row[indexes.level] ?? ''), relevance: row[indexes.relevance] ?? '', publishedAt: excelDateValue(row[indexes.publishedAt]), loadedAt: excelDateValue(row[indexes.loadedAt]), descriptionMd: String(row[indexes.descriptionMd] ?? ''), ...(indexes.coverLetterSubject >= 0 ? { coverLetterSubject: String(optional(row, 'coverLetterSubject')) } : {}), ...(indexes.coverLetterMd >= 0 ? { coverLetterMd: String(optional(row, 'coverLetterMd')) } : {}), url: String(row[indexes.url] ?? '') })).filter(row => Object.values(row).some(value => value !== '' && value !== null));
    if (rows.length > 2000)
        throw new Error('За один раз можно импортировать не более 2000 вакансий.');
    return rows;
}
