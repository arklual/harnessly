import { stripAnsi } from '@/lib/hl-command-run';

/**
 * The reply Claude has already printed in its terminal but not yet written to the transcript: Claude Code
 * records a message only once the whole API message is done, so a long tool call after a paragraph keeps that paragraph
 * off the timeline for a minute while the person reads it in the terminal. Read from the screen:
 *
 *   ❯ the prompt            ← the last prompt the agent took
 *   ● text of the reply      ← a reply block; its next lines are indented by two spaces
 *     continued…
 *   ● Bash(npm test)         ← a tool call, with ⎿ result lines
 *     ⎿ …
 *   ✻ Effecting… (42s)       ← the spinner
 *   ──────────               ← the prompt box
 *
 * Only the text after the last tool call counts: what came before it is in the transcript already.
 */
const PROMPT_RE = /^❯[ \u00a0]/;
const BOX_RULE_RE = /^─{8,}/;
const BLOCK_RE = /^●[ \u00a0]/;
const TOOL_RE = /^●[ \u00a0][A-Za-z][\w.:-]*\(/;
const SPINNER_RE = /^[·✢✳✶✻✽*][ \u00a0]\S/;
const RESULT_RE = /^\s+⎿/;
const LIST_RE = /^(?:[-*•]|\d+[.)])\s/;
/** A notice the TUI right-aligns over the last line («Update installed · Restart to update») is not part of the reply. */
const RIGHT_ALIGNED_RE = /^ {24,}\S/;

const MAX_LIVE_CHARS = 4_000;

interface IBlock {
  tool: boolean;
  lines: string[];
}

/** Joins the hard-wrapped lines of a paragraph; list items and blank lines keep their own line. */
const unwrap = (lines: readonly string[]): string => {
  const out: string[] = [];
  for (const line of lines) {
    const text = line
      .replace(/^ {2}/, '')
      .replace(/ {8,}\S.*$/, '')
      .trimEnd();
    const last = out.length - 1;
    if (text === '') out.push('');
    else if (last < 0 || out[last] === '' || LIST_RE.test(text) || /^#{1,6}\s/.test(text)) out.push(text);
    else out[last] = `${out[last]} ${text.trimStart()}`;
  }
  return out
    .join('\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
};

export const readLiveReply = (screen: string): string => {
  const lines = stripAnsi(screen).replace(/\r/g, '').split('\n');
  let end = lines.length;
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    if (BOX_RULE_RE.test(lines[i]) && lines.slice(i + 1, i + 3).some((line) => PROMPT_RE.test(line))) {
      end = i;
      break;
    }
  }
  let start = -1;
  for (let i = end - 1; i >= 0; i -= 1) {
    if (PROMPT_RE.test(lines[i])) {
      start = i;
      break;
    }
  }
  if (start < 0) return '';

  const blocks: IBlock[] = [];
  for (let i = start + 1; i < end; i += 1) {
    const line = lines[i];
    if (BLOCK_RE.test(line)) {
      blocks.push({ tool: TOOL_RE.test(line), lines: [line.replace(BLOCK_RE, '')] });
      continue;
    }
    if (SPINNER_RE.test(line) || RIGHT_ALIGNED_RE.test(line)) continue;
    const block = blocks.at(-1);
    if (!block) continue;
    if (RESULT_RE.test(line)) block.tool = true;
    else if (line.startsWith('  ') || line.trim() === '') block.lines.push(line);
  }

  let firstText = blocks.length;
  while (firstText > 0 && !blocks[firstText - 1].tool) firstText -= 1;
  const text = blocks
    .slice(firstText)
    .map((block) => unwrap(block.lines))
    .filter(Boolean)
    .join('\n\n');
  return text.length > MAX_LIVE_CHARS ? `${text.slice(0, MAX_LIVE_CHARS)}…` : text;
};

/** The live text is already on the timeline: the transcript's reply starts with it (wrapping aside). */
export const liveReplyShown = (live: string, transcriptText: string): boolean => {
  const squash = (value: string) => value.replace(/[\s*_`#>]+/g, ' ').trim();
  const head = squash(live).slice(0, 80);
  return head !== '' && squash(transcriptText).includes(head);
};
