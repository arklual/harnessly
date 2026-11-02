// A column's letters and a cell's address: the page needs these without the engine that reads the file.
export const columnName = (index: number): string => {
  let name = '';
  for (let at = index + 1; at > 0; at = Math.floor((at - 1) / 26))
    name = String.fromCharCode(65 + ((at - 1) % 26)) + name;
  return name;
};

export const addressOf = (row: number, column: number): string => `${columnName(column)}${row + 1}`;
