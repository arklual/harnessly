// «Перейти к определению» in the review, as an IDE does it with ⌘/Ctrl: the word under the pointer is looked up as a
// declaration in the files of the change (Go, TypeScript and JavaScript, Python, Rust, Java, Kotlin, CSS variables).
// The change is what the review has; a declaration outside it is not found, and the screen says so.

export interface IDefinition {
  path: string;
  /** 1-based. */
  line: number;
  text: string;
}

const WORD = /--[\w-]+|[A-Za-z_$][\w$]*/;

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

const patternsOf = (name: string): RegExp[] => {
  const n = escape(name);
  return [
    // Go: func Name(, func (r *T) Name(, type Name, var/const Name
    new RegExp(`^\\s*func\\s+(?:\\([^)]*\\)\\s*)?${n}\\s*[[(]`),
    new RegExp(
      `^\\s*(?:export\\s+)?(?:default\\s+)?(?:declare\\s+)?(?:abstract\\s+)?(?:async\\s+)?(?:function\\*?|class|interface|type|enum|const|let|var|struct|trait|impl|object|def|fn|fun|val|module|namespace)\\s+${n}\\b`,
    ),
    // Go's grouped declarations and struct fields' types: `\tName = …`, `\tName Type` inside a block.
    new RegExp(`^\\s+${n}\\s*(?:=|:=)\\s`),
    // Methods of classes and objects: `name(…) {`, `async name(…)`, `name = (…) =>`, `name: (…) =>`.
    new RegExp(
      `^\\s+(?:public\\s+|private\\s+|protected\\s+|static\\s+|readonly\\s+|async\\s+|override\\s+)*${n}\\s*(?:<[^>]*>)?\\([^)]*\\)\\s*(?::[^{]+)?\\{\\s*$`,
    ),
    new RegExp(`^\\s+(?:readonly\\s+|static\\s+)*${n}\\s*[:=]\\s*(?:async\\s*)?(?:\\([^)]*\\)|\\w+)\\s*=>`),
    // CSS custom properties and Python's top-level names.
    new RegExp(`^\\s*${n}\\s*:`),
  ];
};

/** The identifier at a column of a line, or null off a word. */
export const wordAt = (line: string, col: number): string | null => {
  for (const m of line.matchAll(new RegExp(WORD, 'g'))) {
    const from = m.index;
    if (col >= from && col <= from + m[0].length) return m[0];
  }
  return null;
};

const MAX_FOUND = 20;

/** Where the name is declared in the given files: each place once, the file asked from first. */
export const findDefinitions = (
  name: string,
  files: readonly { path: string; text: string }[],
  from?: string,
): IDefinition[] => {
  if (!WORD.test(name) || name.length < 2) return [];
  const patterns = patternsOf(name);
  const ordered = [...files].sort((a, b) => Number(b.path === from) - Number(a.path === from));
  const found: IDefinition[] = [];
  for (const file of ordered) {
    // CSS custom properties are declared with a colon; anywhere else a colon is a field or a label, not a definition.
    const css = name.startsWith('--');
    const lines = file.text.split('\n');
    for (let i = 0; i < lines.length && found.length < MAX_FOUND; i++) {
      const text = lines[i] ?? '';
      const hit = css ? patterns[5]?.test(text) : patterns.slice(0, 5).some((p) => p.test(text));
      if (hit) found.push({ path: file.path, line: i + 1, text: text.trim() });
    }
  }
  return found;
};
