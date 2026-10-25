// The agent's last words as one line of the folded chat: the last line of prose of its latest answer, without the marks
// of its markdown (code, tables and rules are not words to read in a line).

const MARKS = /^\s*(?:#{1,6}\s+|[-*+]\s+|>\s*|\d+[.)]\s+)/;
const INLINE = /(\*\*|__|`+|~~)/g;
const LINK = /\[([^\]]*)\]\([^)]*\)/g;
const RULE = /^[-*_]{3,}$/;

export const lastLineOf = (md: string): string | null => {
  let code = false;
  let last: string | null = null;
  for (const raw of md.split('\n')) {
    const line = raw.trim();
    if (line.startsWith('```')) {
      code = !code;
      continue;
    }
    if (code || line === '' || RULE.test(line) || line.startsWith('|')) continue;
    const plain = line.replace(MARKS, '').replace(LINK, '$1').replace(INLINE, '').trim();
    if (plain !== '') last = plain;
  }
  return last;
};
