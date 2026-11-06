// The arithmetic of a counting number: the numbers inside a formatted string move from one value to the other, the rest
// of the string (units, signs, spaces) is the new one's. It reads the shape of a number, not the meaning of a word.
export const DUR_MS = 650;

const NUM = /\d+(?:[  ]\d{3})*(?:[.,]\d+)?/g;

const parse = (tok: string): { n: number; dec: number; sep: string; dot: string } => {
  const m = /[.,](\d+)$/.exec(tok);
  const dec = m?.[1]?.length ?? 0;
  const dot = m ? tok.charAt(tok.length - dec - 1) : '.';
  const sep = /[  ]/.exec(tok)?.[0] ?? '';
  const clean = tok.replace(/[  ]/g, '').replace(',', '.');
  return { n: Number(clean), dec, sep, dot };
};

const group = (int: string, sep: string): string => (sep ? int.replace(/\B(?=(\d{3})+(?!\d))/g, sep) : int);

/** The string `to` with each of its numbers replaced by the one k (0..1) of the way from the same-place number of `from`. */
export const tweenText = (from: string, to: string, k: number): string => {
  const a = from.match(NUM) ?? [];
  const b = to.match(NUM) ?? [];
  if (a.length !== b.length || b.length === 0) return to;
  let i = 0;
  return to.replace(NUM, () => {
    const x = parse(a[i] ?? '0');
    const y = parse(b[i] ?? '0');
    i += 1;
    const v = x.n + (y.n - x.n) * k;
    const fixed = Math.abs(v).toFixed(y.dec);
    const [int = '0', frac] = fixed.split('.');
    return group(int, y.sep) + (frac ? y.dot + frac : '');
  });
};
