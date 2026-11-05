// The files another agent's report names: a path in inline code (`/home/…/review.md`, `web/app/x.ts:12`). There the
// path is a chip with the file's name and opens in the pane; the agent's own text keeps its code as code.

const CODE_PATH = /^(?:~?\/)?(?:[\w@.-]+\/)+[\w@-]+(?:\.[\w-]+)+(?::\d+)?$/;
const INLINE_CODE = /`([^`\n]+)`/g;
const LINE_SUFFIX = /:\d+$/;

/** A path in inline code of a report. */
export const isReportPath = (text: string): boolean => CODE_PATH.test(text);

/** «/tmp/…/research/inventory.md:12» → «inventory.md»: what the chip says. */
export const fileNameOf = (path: string): string => {
  const bare = path.replace(LINE_SUFFIX, '');
  return bare.slice(bare.lastIndexOf('/') + 1) || bare;
};

/** The distinct files a report names in inline code, in the order it names them. */
export const pathsOf = (md: string): string[] => {
  const out = new Set<string>();
  for (const m of md.matchAll(INLINE_CODE)) {
    const text = m[1].trim();
    if (isReportPath(text)) out.add(text);
  }
  return [...out];
};

/** The report with each path in inline code cut to its file's name, for the folded line. */
export const withFileNames = (md: string): string =>
  md.replace(INLINE_CODE, (all, text: string) =>
    isReportPath(text.trim()) ? `\`${fileNameOf(text.trim())}\`` : all,
  );
