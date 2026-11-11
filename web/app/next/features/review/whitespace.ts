// A diff that ignores whitespace, for the merge view's `diffConfig.override`: lines are compared with their spaces
// collapsed and trimmed, so a re-indented block is not a change; within lines that did change, the inline highlight is
// the package's own character diff. Lines are diffed by turning each distinct line into one character and diffing those
// strings with the package's own algorithm.
import { Change, diff, presentableDiff } from '@codemirror/merge';

export const normalizeLine = (line: string): string => line.trim().replace(/\s+/g, ' ');

const FIRST_CODE = 0x100;
const SURROGATES = 0xd800;
const PRIVATE_USE = 0xe000;
const LAST_CODE = 0xffff;

/** Where each line starts, plus the text's length past the last one. */
const lineStarts = (lines: readonly string[]): number[] => {
  const out: number[] = [];
  let at = 0;
  for (const line of lines) {
    out.push(at);
    at += line.length + 1;
  }
  out.push(at - 1);
  return out;
};

export const ignoreWhitespaceDiff = (a: string, b: string): readonly Change[] => {
  const linesA = a.split('\n');
  const linesB = b.split('\n');
  const codes = new Map<string, string>();
  let next = FIRST_CODE;
  let overflow = false;
  const encode = (lines: readonly string[]) =>
    lines
      .map((line) => {
        const key = normalizeLine(line);
        let code = codes.get(key);
        if (code === undefined) {
          if (next === SURROGATES) next = PRIVATE_USE;
          if (next > LAST_CODE) {
            overflow = true;
            return '';
          }
          code = String.fromCharCode(next++);
          codes.set(key, code);
        }
        return code;
      })
      .join('');
  const encodedA = encode(linesA);
  const encodedB = encode(linesB);
  if (overflow) return presentableDiff(a, b);

  const startsA = lineStarts(linesA);
  const startsB = lineStarts(linesB);
  const out: Change[] = [];
  for (const line of diff(encodedA, encodedB)) {
    const fromA = startsA[line.fromA]!;
    const toA = line.toA > line.fromA ? startsA[line.toA]! : fromA;
    const fromB = startsB[line.fromB]!;
    const toB = line.toB > line.fromB ? startsB[line.toB]! : fromB;
    if (fromA === toA || fromB === toB) {
      out.push(new Change(fromA, toA, fromB, toB));
      continue;
    }
    for (const inner of presentableDiff(a.slice(fromA, toA), b.slice(fromB, toB)))
      out.push(new Change(fromA + inner.fromA, fromA + inner.toA, fromB + inner.fromB, fromB + inner.toB));
  }
  return out;
};
