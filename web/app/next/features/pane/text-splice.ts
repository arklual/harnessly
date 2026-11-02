// Editing text that an office file keeps in pieces with their own looks (the runs of a paragraph). Only what the person
// changed moves, so the words they did not touch keep their looks. Pure, on the file's own XML nodes.

/** Replaces the pieces' joined text with `next`: the common start and end stay in their pieces, the new middle goes into the piece it starts in. */
export const spliceText = (pieces: readonly Element[], next: string): void => {
  const before = pieces.map((piece) => piece.textContent ?? '').join('');
  if (before === next || pieces.length === 0) return;
  let start = 0;
  const shortest = Math.min(before.length, next.length);
  while (start < shortest && before[start] === next[start]) start += 1;
  let end = 0;
  while (end < shortest - start && before[before.length - 1 - end] === next[next.length - 1 - end]) end += 1;
  const middle = next.slice(start, next.length - end);
  const cutTo = before.length - end;
  let at = 0;
  let placed = false;
  for (const piece of pieces) {
    const text = piece.textContent ?? '';
    const from = at;
    const to = at + text.length;
    at = to;
    const keepHead = text.slice(0, Math.max(0, Math.min(text.length, start - from)));
    const keepTail = text.slice(Math.max(0, Math.min(text.length, cutTo - from)));
    // The new words go where the change starts: at the end of the piece that holds the start, or in the first one.
    const here = !placed && (start <= to || piece === pieces[pieces.length - 1]);
    piece.textContent = keepHead + (here ? middle : '') + keepTail;
    if (here) placed = true;
  }
};
