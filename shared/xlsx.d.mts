export function buildXlsx(
  sheets: {
    name: string;
    rows: string[][];
    numericColumns?: Set<number>;
    columns?: string;
    filterEndColumn?: string;
  }[],
  options?: { wrapAndAlignTop?: boolean },
): Uint8Array<ArrayBuffer>;
