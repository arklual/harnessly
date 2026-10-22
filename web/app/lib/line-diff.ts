// A line diff of two texts, written as a unified diff: what the timeline shows for an edit the agent made.
export interface IDiffLine {
  kind: ' ' | '+' | '-';
  text: string;
}

/** Past this many cells of the comparison table the middle of the text is shown as replaced as a whole. */
const MAX_CELLS = 4_000_000;
const CONTEXT = 3;

const splitLines = (text: string): string[] => {
  if (text === '') return [];
  const lines = text.split('\n');
  if (lines[lines.length - 1] === '') lines.pop();
  return lines;
};

/** Lines of `a` and `b` aligned: the common start and end are cut off, the middle is compared by longest common subsequence. */
export const diffLines = (oldText: string, newText: string): IDiffLine[] => {
  const a = splitLines(oldText);
  const b = splitLines(newText);
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start += 1;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA -= 1;
    endB -= 1;
  }
  const out: IDiffLine[] = a.slice(0, start).map((text) => ({ kind: ' ', text }));
  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);
  const rows = midA.length;
  const cols = midB.length;
  if (rows * cols > MAX_CELLS || rows === 0 || cols === 0) {
    for (const text of midA) out.push({ kind: '-', text });
    for (const text of midB) out.push({ kind: '+', text });
  } else {
    // lcs[i][j]: the common length of midA[i..] and midB[j..]
    const width = cols + 1;
    const lcs = new Uint32Array((rows + 1) * width);
    for (let i = rows - 1; i >= 0; i -= 1) {
      for (let j = cols - 1; j >= 0; j -= 1) {
        lcs[i * width + j] =
          midA[i] === midB[j]
            ? lcs[(i + 1) * width + j + 1]! + 1
            : Math.max(lcs[(i + 1) * width + j]!, lcs[i * width + j + 1]!);
      }
    }
    let i = 0;
    let j = 0;
    while (i < rows && j < cols) {
      if (midA[i] === midB[j]) {
        out.push({ kind: ' ', text: midA[i]! });
        i += 1;
        j += 1;
      } else if (lcs[(i + 1) * width + j]! >= lcs[i * width + j + 1]!) {
        out.push({ kind: '-', text: midA[i]! });
        i += 1;
      } else {
        out.push({ kind: '+', text: midB[j]! });
        j += 1;
      }
    }
    for (; i < rows; i += 1) out.push({ kind: '-', text: midA[i]! });
    for (; j < cols; j += 1) out.push({ kind: '+', text: midB[j]! });
  }
  for (const text of a.slice(endA)) out.push({ kind: ' ', text });
  return out;
};

export const diffStat = (lines: IDiffLine[]): { added: number; removed: number } => ({
  added: lines.filter((l) => l.kind === '+').length,
  removed: lines.filter((l) => l.kind === '-').length,
});

/** The change as a one-file unified diff with three lines of context; empty when nothing differs. */
export const unifiedDiff = (filePath: string, oldText: string, newText: string): string => {
  const lines = diffLines(oldText, newText);
  if (!lines.some((l) => l.kind !== ' ')) return '';
  const path = filePath || 'file';
  const isNew = oldText === '';
  const head = [`diff --git a/${path} b/${path}`];
  if (isNew) head.push('new file mode 100644');
  head.push(isNew ? '--- /dev/null' : `--- a/${path}`, `+++ b/${path}`);

  // Changed lines, each widened by the context, merged into hunks.
  const keep = new Array<boolean>(lines.length).fill(false);
  lines.forEach((line, index) => {
    if (line.kind === ' ') return;
    for (let k = Math.max(0, index - CONTEXT); k <= Math.min(lines.length - 1, index + CONTEXT); k += 1)
      keep[k] = true;
  });
  const before: number[] = [];
  const after: number[] = [];
  let oldNo = 1;
  let newNo = 1;
  for (const line of lines) {
    before.push(oldNo);
    after.push(newNo);
    if (line.kind !== '+') oldNo += 1;
    if (line.kind !== '-') newNo += 1;
  }
  const body: string[] = [];
  let index = 0;
  while (index < lines.length) {
    if (!keep[index]) {
      index += 1;
      continue;
    }
    const from = index;
    while (index < lines.length && keep[index]) index += 1;
    const slice = lines.slice(from, index);
    const oldCount = slice.filter((l) => l.kind !== '+').length;
    const newCount = slice.filter((l) => l.kind !== '-').length;
    const oldStart = oldCount === 0 ? before[from]! - 1 : before[from]!;
    const newStart = newCount === 0 ? after[from]! - 1 : after[from]!;
    body.push(`@@ -${oldStart},${oldCount} +${newStart},${newCount} @@`);
    for (const line of slice) body.push(`${line.kind}${line.text}`);
  }
  return `${[...head, ...body].join('\n')}\n`;
};
