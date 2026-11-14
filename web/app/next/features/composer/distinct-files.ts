// One paste, one attachment: a clipboard can hold the same picture twice — its pixels and a copy of the file
// it came from (a messenger, a file manager) — and the browser hands over both. Files with the same bytes are one.

/** The bytes of a file, to tell two of the same size apart. */
const bytesOf = async (file: File): Promise<Uint8Array> => new Uint8Array(await file.arrayBuffer());

const sameBytes = (a: Uint8Array, b: Uint8Array): boolean =>
  a.length === b.length && a.every((byte, i) => byte === b[i]);

/** The files with duplicates by content left out, in the order they came. */
export const distinctFiles = async (files: readonly File[]): Promise<File[]> => {
  if (files.length < 2) return [...files];
  const kept: { file: File; bytes: Uint8Array | null }[] = [];
  for (const file of files) {
    let twin = false;
    for (const have of kept) {
      if (have.file.size !== file.size) continue;
      have.bytes ??= await bytesOf(have.file);
      if (sameBytes(have.bytes, await bytesOf(file))) {
        twin = true;
        break;
      }
    }
    if (!twin) kept.push({ file, bytes: null });
  }
  return kept.map((k) => k.file);
};
