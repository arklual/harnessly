// A unified diff as review files, and the two sides of one file rebuilt from its hunks when the whole versions are not
// available: the lines between hunks are unknown and stay blank, so the line numbers still match the real file.
import {
  FILE_STATUS,
  type IFileContent,
  type IReviewFile,
  type TFileStatus,
} from '@/next/features/review/model';

/** FNV-1a, 32 bits, as 8 hex digits: a cheap fingerprint of a file's patch. */
export const hashText = (text: string): string => {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return (h >>> 0).toString(16).padStart(8, '0');
};

const statusOf = (isNew: boolean, isDeleted: boolean, oldPath: string, newPath: string): TFileStatus => {
  if (isNew) return FILE_STATUS.added;
  if (isDeleted) return FILE_STATUS.deleted;
  if (oldPath !== newPath) return FILE_STATUS.renamed;
  return FILE_STATUS.modified;
};

/** The hunks only: the header lines (`index`, mode) differ between sources for the same change. */
const hunksOf = (patch: string): string => {
  const at = patch.search(/^@@/m);
  return at < 0 ? '' : patch.slice(at);
};

interface IPatchFile {
  oldPath: string;
  newPath: string;
  isNew: boolean;
  isDeleted: boolean;
  isBinary: boolean;
  additions: number;
  deletions: number;
  raw: string;
}

const unquote = (p: string) => p.replace(/^"|"$/g, '').replace(/^[ab]\//, '');

/** The files of a multi-file unified diff, each with its part of the text from its `diff --git` line. */
export const parsePatch = (input: string): IPatchFile[] =>
  input
    .split(/^(?=diff --git )/m)
    .filter((chunk) => chunk.startsWith('diff --git '))
    .map((chunk) => {
      const head = /^diff --git (\S+|"[^"]+") (\S+|"[^"]+")/.exec(chunk);
      let oldPath = head ? unquote(head[1]!) : '';
      let newPath = head ? unquote(head[2]!) : '';
      let additions = 0;
      let deletions = 0;
      let inHunk = false;
      let fromNothing = false;
      let toNothing = false;
      for (const line of chunk.split('\n')) {
        if (line.startsWith('@@')) inHunk = true;
        else if (!inHunk && line === '--- /dev/null') fromNothing = true;
        else if (!inHunk && line === '+++ /dev/null') toNothing = true;
        else if (!inHunk && line.startsWith('--- ')) oldPath = unquote(line.slice(4));
        else if (!inHunk && line.startsWith('+++ ')) newPath = unquote(line.slice(4));
        else if (inHunk && line.startsWith('+')) additions += 1;
        else if (inHunk && line.startsWith('-')) deletions += 1;
      }
      return {
        oldPath,
        newPath,
        isNew: fromNothing || /^new file mode/m.test(chunk),
        isDeleted: toNothing || /^deleted file mode/m.test(chunk),
        isBinary: /^Binary files /m.test(chunk) || /^GIT binary patch/m.test(chunk),
        additions,
        deletions,
        raw: chunk,
      };
    });

export const filesFromPatch = (diff: string): IReviewFile[] =>
  parsePatch(diff).map((file) => {
    const status = statusOf(file.isNew, file.isDeleted, file.oldPath, file.newPath);
    const path = status === FILE_STATUS.deleted ? file.oldPath : file.newPath;
    return {
      path,
      oldPath: file.oldPath,
      status,
      additions: file.additions,
      deletions: file.deletions,
      binary: file.isBinary,
      patch: file.raw,
      version: hashText(`${file.oldPath}\0${file.newPath}\0${hunksOf(file.raw)}`),
    };
  });

const HUNK = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

const pad = (lines: string[], upTo: number) => {
  while (lines.length < upTo) lines.push('');
};

/** Both sides of one file from its patch; unknown lines are blank. */
export const sidesFromPatch = (patch: string): IFileContent => {
  const before: string[] = [];
  const after: string[] = [];
  let inHunk = false;
  for (const line of patch.split('\n')) {
    const head = HUNK.exec(line);
    if (head) {
      inHunk = true;
      pad(before, Number(head[1]) - 1);
      pad(after, Number(head[3]) - 1);
      continue;
    }
    if (!inHunk) continue;
    if (line.startsWith('\\')) continue; // "\ No newline at end of file"
    const mark = line[0];
    const text = line.slice(1);
    if (mark === '+') after.push(text);
    else if (mark === '-') before.push(text);
    else if (mark === ' ' || line === '') {
      before.push(text);
      after.push(text);
    }
  }
  // The split above leaves one empty line after the final newline of the patch.
  if (patch.endsWith('\n') && before.at(-1) === '' && after.at(-1) === '') {
    before.pop();
    after.pop();
  }
  return { before: before.join('\n'), after: after.join('\n'), complete: false };
};
