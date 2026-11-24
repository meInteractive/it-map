import { buildXlsx } from '../../../shared/xlsx.mjs';

interface ExportData {
  applications: {
    id: string;
    date: string;
    status: string;
    company: string;
    vacancyId: string;
    vacancyTitle: string;
    resume: { id: number; name: string };
  }[];
  vacancies: { id: string; title: string; description: string }[];
  resumes: { id: number; name: string; markdown: string }[];
}

const excelCellLimit = 32767;
const excelCellLineFeeds = 253;
const xlsxMime = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

// Excel ограничивает размер ячейки. Делим длинный текст без потери символов.
function textParts(text: string) {
  const parts: string[] = [];
  let start = 0;
  while (start < text.length) {
    let end = Math.min(start + excelCellLimit, text.length);
    if (end < text.length && text.charCodeAt(end) >= 0xdc00 && text.charCodeAt(end) <= 0xdfff)
      end--;
    let nextLF = text.indexOf('\n', start);
    for (let count = 0; count < excelCellLineFeeds && nextLF >= 0 && nextLF < end; count++) {
      nextLF = text.indexOf('\n', nextLF + 1);
    }
    if (nextLF >= 0 && nextLF < end) end = nextLF;
    parts.push(text.slice(start, end));
    start = end;
  }
  return parts.length ? parts : [''];
}

function textTable(header: string[], rows: string[][], textColumn: string) {
  const split = rows.map((row) => [...row.slice(0, -1), ...textParts(row.at(-1) ?? '')]);
  const width = split.reduce((max, row) => Math.max(max, row.length - header.length), 1);
  const labels = [textColumn];
  for (let i = 2; i <= width; i++) labels.push(textColumn + ' — часть ' + i);
  return [[...header, ...labels], ...split];
}

export function applicationsWorkbook(data: ExportData) {
  return buildXlsx([
    {
      name: 'Отклики',
      rows: [
        [
          'ID отклика',
          'Дата отклика',
          'Текущий статус',
          'Компания',
          'ID вакансии',
          'Название вакансии',
          'ID резюме',
          'Название резюме',
        ],
        ...data.applications.map((row) => [
          row.id,
          row.date,
          row.status,
          row.company,
          row.vacancyId,
          row.vacancyTitle,
          String(row.resume.id),
          row.resume.name,
        ]),
      ],
      columns: '<col min="1" max="8" width="23" customWidth="1"/>',
    },
    {
      name: 'Вакансии',
      rows: textTable(
        ['ID вакансии', 'Название вакансии'],
        data.vacancies.map((row) => [row.id, row.title, row.description]),
        'Описание вакансии',
      ),
      columns:
        '<col min="1" max="2" width="25" customWidth="1"/><col min="3" max="16384" width="80" customWidth="1"/>',
    },
    {
      name: 'Резюме',
      rows: textTable(
        ['ID резюме', 'Название резюме'],
        data.resumes.map((row) => [String(row.id), row.name, row.markdown]),
        'Текст резюме',
      ),
      columns:
        '<col min="1" max="2" width="25" customWidth="1"/><col min="3" max="16384" width="80" customWidth="1"/>',
    },
  ]);
}

export function downloadApplications(data: ExportData) {
  const blob = new Blob([applicationsWorkbook(data)], { type: xlsxMime });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = 'applications.xlsx';
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
