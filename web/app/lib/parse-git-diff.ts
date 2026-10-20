// A unified diff of several files split per file, with added and removed line counts.
export interface IGitDiffFile {
  oldPath: string;
  newPath: string;
  isNew: boolean;
  isDeleted: boolean;
  isBinary: boolean;
  additions: number;
  deletions: number;
  /** This file's part of the diff, from its `diff --git` line. */
  raw: string;
}

const unquote = (p: string) => p.replace(/^"|"$/g, '').replace(/^[ab]\//, '');

export const parseMultiFileDiff = (input: string): IGitDiffFile[] => {
  const files: IGitDiffFile[] = [];
  const chunks = input.split(/^(?=diff --git )/m).filter((c) => c.startsWith('diff --git '));
  for (const chunk of chunks) {
    const head = /^diff --git (\S+|"[^"]+") (\S+|"[^"]+")/.exec(chunk);
    let oldPath = head ? unquote(head[1]!) : '';
    let newPath = head ? unquote(head[2]!) : '';
    let additions = 0;
    let deletions = 0;
    let inHunk = false;
    for (const line of chunk.split('\n')) {
      if (line.startsWith('@@')) inHunk = true;
      else if (!inHunk && line.startsWith('--- ') && line !== '--- /dev/null')
        oldPath = unquote(line.slice(4));
      else if (!inHunk && line.startsWith('+++ ') && line !== '+++ /dev/null')
        newPath = unquote(line.slice(4));
      else if (inHunk && line.startsWith('+')) additions += 1;
      else if (inHunk && line.startsWith('-')) deletions += 1;
    }
    files.push({
      oldPath,
      newPath,
      isNew: /^new file mode/m.test(chunk),
      isDeleted: /^deleted file mode/m.test(chunk),
      isBinary: /^Binary files /m.test(chunk) || /^GIT binary patch/m.test(chunk),
      additions,
      deletions,
      raw: chunk,
    });
  }
  return files;
};
