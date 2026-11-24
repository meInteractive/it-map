export function xmlEscape(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\"/g, "&quot;")
    .replace(/'/g, "&apos;")
    .replace(/\r/g, "&#13;");
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


const spreadsheet = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const officeRelationships = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const packageRelationships = 'http://schemas.openxmlformats.org/package/2006/relationships';
const contentTypes = 'http://schemas.openxmlformats.org/package/2006/content-types';
const xlsxType = 'application/vnd.openxmlformats-officedocument.spreadsheetml';
const xml = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';
const styles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1F2937"/><bgColor rgb="FF1F2937"/></patternFill></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1" applyAlignment="1"><alignment vertical="center"/></xf></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`;

function excelText(value) {
  return String(value ?? '')
    .replace(/_(?=x[0-9A-Fa-f]{4}_)/gi, '_x005F_')
    .replace(/[\u0000-\u0008\u000B-\u000C\u000E-\u001F\uFFFE\uFFFF]/g,
      (character) => '_x' + character.charCodeAt(0).toString(16).padStart(4, '0').toUpperCase() + '_');
}

export function buildXlsx(sheets) {
  const files = sheets.map(({ name, rows, numericColumns, columns, filterEndColumn }, index) => {
    const data = rows.map((row, rowIndex) =>
      `<row r="${rowIndex + 1}"${rowIndex === 0 ? ' ht="30" customHeight="1"' : ''}>${row.map((value, column) =>
        xlsxCell(xlsxColumnName(column + 1) + (rowIndex + 1), excelText(value), rowIndex === 0 ? 1 : 0,
          rowIndex > 0 && numericColumns?.has(column) && value !== '')
      ).join('')}</row>`
    ).join('');
    const lastColumn = filterEndColumn || xlsxColumnName(rows[0].length);
    const view = '<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>';
    return {
      name,
      id: index + 1,
      path: `worksheets/sheet${index + 1}.xml`,
      body: `${xml}<worksheet xmlns="${spreadsheet}">${view}${columns ? '<cols>' + columns + '</cols>' : ''}<sheetData>${data}</sheetData><autoFilter ref="A1:${lastColumn}${rows.length}"/></worksheet>`,
    };
  });
  const overrides = files.map(({path}) => `<Override PartName="/xl/${path}" ContentType="${xlsxType}.worksheet+xml"/>`).join('');
  const workbook = files.map(({name, id}) => `<sheet name="${xmlEscape(name)}" sheetId="${id}" r:id="rId${id}"/>`).join('');
  const relationships = files.map(({id, path}) => `<Relationship Id="rId${id}" Type="${officeRelationships}/worksheet" Target="${path}"/>`).join('');
  return zipStored([
    ['[Content_Types].xml', `${xml}<Types xmlns="${contentTypes}"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="${xlsxType}.sheet.main+xml"/>${overrides}<Override PartName="/xl/styles.xml" ContentType="${xlsxType}.styles+xml"/></Types>`],
    ['_rels/.rels', `${xml}<Relationships xmlns="${packageRelationships}"><Relationship Id="rId1" Type="${officeRelationships}/officeDocument" Target="xl/workbook.xml"/></Relationships>`],
    ['xl/workbook.xml', `${xml}<workbook xmlns="${spreadsheet}" xmlns:r="${officeRelationships}"><sheets>${workbook}</sheets></workbook>`],
    ['xl/_rels/workbook.xml.rels', `${xml}<Relationships xmlns="${packageRelationships}">${relationships}<Relationship Id="rId${files.length + 1}" Type="${officeRelationships}/styles" Target="styles.xml"/></Relationships>`],
    ['xl/styles.xml', styles],
    ...files.map(({path, body}) => ['xl/' + path, body]),
  ]);
}
