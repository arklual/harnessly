// Reading a numbered choice menu off the agent's screen ("❯ 1. Yes", "  2. No, and tell Claude…").
const NUMBERED = /^\s*(?:[❯>›]\s*)?(\d+)[.)]\s+(.*\S)\s*$/;

/** The number in front of an option label, if it has one. */
export const optionNumber = (label: string): string | null => NUMBERED.exec(label)?.[1] ?? null;

/** The label without its number. */
export const stripNumberPrefix = (label: string): string => {
  const m = NUMBERED.exec(label);
  return m ? m[2]! : label.trim();
};

export interface IParsedPromptOptions {
  /** The question above the menu, when the screen shows one. */
  title?: string;
  options: string[];
  /** Index of the highlighted option (the cursor), or -1. */
  focusedIndex: number;
}

/**
 * The options of the last numbered menu on the screen: consecutive lines numbered 1, 2, 3… Lines that continue an
 * option (indented text under it) are skipped. The highlighted one is marked with ❯, > or ›.
 */
export const parseChoiceOptions = (screen: string): IParsedPromptOptions => {
  const lines = screen.split('\n');
  let best: IParsedPromptOptions = { options: [], focusedIndex: -1 };
  let run: string[] = [];
  let focused = -1;
  let expect = 1;
  const close = () => {
    if (run.length >= 2) best = { options: run, focusedIndex: focused };
    run = [];
    focused = -1;
    expect = 1;
  };
  for (const line of lines) {
    const m = NUMBERED.exec(line);
    if (m && Number(m[1]) === expect) {
      if (/^\s*[❯>›]/.test(line)) focused = run.length;
      run.push(`${m[1]}. ${m[2]}`);
      expect += 1;
    } else if (m && Number(m[1]) === 1) {
      close();
      if (/^\s*[❯>›]/.test(line)) focused = 0;
      run.push(`1. ${m[2]}`);
      expect = 2;
    } else if (run.length && line.trim() && !/^\s{3,}\S/.test(line)) {
      close();
    }
  }
  close();
  return best;
};
