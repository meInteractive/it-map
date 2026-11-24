export function buildXlsx(
  sheets: {
    name: string;
    rows: string[][];
    numericColumns?: Set<number>;
    columns?: string;
    filterEndColumn?: string;
  }[],
): Uint8Array<ArrayBuffer>;
