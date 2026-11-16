// what a tool step says in its one line, in words — «Собирает пакет  pnpm build», «Прочитал  …/chat/steps.tsx
// строки 120–180», «youtrack · search issues» — and how it ended (time, exit code, the last lines of its output).
// Pure: the feed's step (steps.tsx) and the group's head draw what these return.
import type { ToolEntry } from '@/next/features/chat/types';
import { elapsed } from '@/next/i18n/format';
import { plural, t } from '@/next/i18n';

export type TStepKind = 'run' | 'read' | 'edit' | 'create' | 'search' | 'web' | 'mcp' | 'other';

/** The line of a step: the words (the agent's description or a verb), a secondary mono part and a short fact. */
export interface IStepLine {
  kind: TStepKind;
  /** The agent's own words for what the step does (a command's description); null when it gave none. */
  title: string | null;
  /** What the step works on, short: the command's meaningful part, the file, the pattern, the query. */
  detail: string;
  /** A short fact of the call: «+12 −3», «строки 10–50», «найдено: 4»; null when there is none. */
  fact: string | null;
  /** The whole command of a shell step, for the opened step and its copy button; null for any other. */
  command: string | null;
}

/** How long a finished step must have run to show its time: a quick one says nothing. */
export const SHOW_DURATION_FROM_MS = 1000;

const MCP_RE = /^mcp__(.+?)__(.+)$/;

/** «plugin_acme-tracker_youtrack» → «youtrack», «acme-preview» stays: the server as a person knows it. */
export const mcpServerName = (server: string): string => {
  const plain = server.replace(/^plugin_/, '').replace(/^claude_ai_/, '');
  const parts = plain.split('_').filter(Boolean);
  return (parts.at(-1) ?? plain).replace(/_/g, ' ');
};

/** «search_issues» → «search issues», «browserSnapshot» → «browser snapshot». */
export const mcpToolName = (tool: string): string =>
  tool
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/[_-]+/g, ' ')
    .toLowerCase()
    .trim();

/** A shell call's own command, as the agent ran it (Codex wraps it in «bash -lc '…'»). */
const unwrapShell = (command: string): string => {
  const m = /^\s*(?:\/(?:usr\/)?bin\/)?(?:ba|z)?sh\s+-l?c\s+([\s\S]+)$/.exec(command);
  if (!m) return command;
  const inner = m[1].trim();
  const quote = inner[0];
  if ((quote === "'" || quote === '"') && inner.endsWith(quote) && inner.length >= 2)
    return inner.slice(1, -1);
  return inner;
};

/** The command split at «&&», «||» and «;» outside quotes, each part with the separator that follows it. */
const splitSegments = (line: string): { text: string; sep: string }[] => {
  const out: { text: string; sep: string }[] = [];
  let quote: string | null = null;
  let start = 0;
  for (let i = 0; i < line.length; i += 1) {
    const ch = line[i];
    if (quote) {
      if (ch === '\\' && quote === '"') i += 1;
      else if (ch === quote) quote = null;
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      continue;
    }
    const two = line.slice(i, i + 2);
    const sep = two === '&&' || two === '||' ? two : ch === ';' ? ';' : '';
    if (!sep) continue;
    out.push({ text: line.slice(start, i).trim(), sep });
    i += sep.length - 1;
    start = i + 1;
  }
  out.push({ text: line.slice(start).trim(), sep: '' });
  return out.filter((seg) => seg.text !== '');
};

const ASSIGN = String.raw`[A-Za-z_][A-Za-z0-9_]*=(?:'[^']*'|"(?:[^"\\]|\\.)*"|\S*)`;
/** A part that only sets things up: an assignment, a cd, an export, a set, a source. */
const SETUP_RE = new RegExp(
  String.raw`^(?:(?:export\s+)?${ASSIGN}(?:\s+${ASSIGN})*|cd(?:\s+\S+)?|pushd\s+\S+|set\s+-\S+|(?:source|\.)\s+\S+|unset\s+.+)$`,
);
/** Assignments in front of a command («FOO=1 BAR=2 go test») — the command is what tells it apart. */
const LEADING_ASSIGN_RE = new RegExp(String.raw`^(?:${ASSIGN}\s+)+(?=\S)`);

/** The longest a path in a command's line stays whole; a longer one keeps its start and its end. */
const PATH_MAX = 36;

/** «/tmp/claude-995/-srv-acme-design-…/56e4efc7-…/run.sh» → «/tmp/…/56e4efc7-2442-…/run.sh»: from the middle. */
export const shortenPath = (token: string, max = PATH_MAX): string => {
  if (token.length <= max || !token.includes('/')) return token;
  const parts = token.split('/');
  const last = parts.at(-1) ?? '';
  const head = parts[0] === '' ? `/${parts[1] ?? ''}` : parts[0];
  const prev = parts.length > 3 ? (parts.at(-2) ?? '') : '';
  const clip = (s: string, n: number) => (s.length > n ? `${s.slice(0, Math.max(1, n - 1))}…` : s);
  const tail = prev ? `${clip(prev, 14)}/${last}` : last;
  return `${clip(head, 12)}/…/${tail}`;
};

/** A command's meaningful part: without the set-up in front (assignments, cd), long paths cut in the middle. */
export const compactCommand = (command: string): string => {
  const first = unwrapShell(command)
    .split('\n')
    .map((line) => line.trim())
    .find((line) => line !== '' && !line.startsWith('#'));
  if (!first) return command.trim();
  const segments = splitSegments(first);
  let from = 0;
  while (from < segments.length - 1 && SETUP_RE.test(segments[from].text)) from += 1;
  const kept = segments.slice(from);
  const line = kept
    .map((seg, index) => {
      const text = index === 0 ? seg.text.replace(LEADING_ASSIGN_RE, '') : seg.text;
      return index === kept.length - 1 || !seg.sep ? text : `${text} ${seg.sep}`;
    })
    .join(' ');
  return line.replace(/\S+/g, (token) => shortenPath(token));
};

/** A file's path as a step shows it: the end, which tells files apart, with «…/» for what is left out. */
export const shortFile = (path: string): string => {
  const parts = path.split('/').filter(Boolean);
  if (path.length <= 48 || parts.length <= 3) return path;
  return `…/${parts.slice(-3).join('/')}`;
};

const EDIT_RE = /^(Update|Create) (.+) \(\+(\d+), -(\d+)\)$/;
const GREP_RE = /^(?:Grep|Glob) "(.*)"(?: → (\d+) results)?$/s;
const LINES_RE = /^(.*) → (\d+) lines$/s;
const FOUND_RE = /^Found (\d+) (?:files?|lines?|matches|occurrences)/;

const readFact = (entry: ToolEntry): string | null => {
  const call = entry.call;
  const from = call?.offset ?? 0;
  const count = call?.limit ?? 0;
  if (from > 0 && count > 0) return t('next.tool.linesRange', { from, to: from + count - 1 });
  if (from > 0) return t('next.tool.linesFrom', { from });
  if (count > 0) return t('next.tool.linesFirst', { n: count });
  if (call?.lines && entry.ok) return plural(call.lines, 'next.tool.linesRead');
  return null;
};

const foundFact = (entry: ToolEntry, count: string | undefined): string | null => {
  if (entry.ok !== true) return null;
  const head = /^\s*([^\n]*)/.exec(entry.output?.text ?? '')?.[1] ?? '';
  const found = FOUND_RE.exec(head)?.[1];
  const n = Number(found ?? count ?? NaN);
  if (Number.isNaN(n)) return null;
  if (n === 0 || /^No (files|matches) found/.test(head)) return t('next.tool.nothingFound');
  return t('next.tool.found', { n });
};

const toolName = (tool: string): string => tool.replace(/^mcp__\w+?__/, '');

/** What a step says in its line: the kind of call, its words, its subject and a short fact. */
export const stepLine = (entry: ToolEntry, detail: string): IStepLine => {
  const name = toolName(entry.tool);
  const call = entry.call;
  const mcp = MCP_RE.exec(entry.tool);
  if (/^(bash|shell|exec|local_shell|run)/i.test(name) && !mcp) {
    const whole = call?.command ?? detail.replace(/^\$ /, '');
    const lines = LINES_RE.exec(whole);
    const command = lines ? lines[1] : whole;
    return {
      kind: 'run',
      title: call?.description?.trim() || null,
      detail: compactCommand(command),
      fact: null,
      command: command.trim() || null,
    };
  }
  if (/^(read|view|cat|notebookread)/i.test(name) && !mcp) {
    const path = detail.replace(/^Read /, '');
    return { kind: 'read', title: null, detail: shortFile(path), fact: readFact(entry), command: null };
  }
  const edit = EDIT_RE.exec(detail);
  if (edit) {
    const created = edit[1] === 'Create';
    return {
      kind: created ? 'create' : 'edit',
      title: null,
      detail: shortFile(edit[2]),
      fact: created ? plural(Number(edit[3]), 'next.tool.linesNew') : `+${edit[3]} −${edit[4]}`,
      command: null,
    };
  }
  if (/^write$/i.test(name)) {
    return {
      kind: 'create',
      title: null,
      detail: shortFile(detail.replace(/^Write /, '')),
      fact: null,
      command: null,
    };
  }
  if (/^(edit|multiedit|apply_patch|notebookedit|str_replace)/i.test(name)) {
    return { kind: 'edit', title: null, detail: shortFile(detail), fact: null, command: null };
  }
  const grep = GREP_RE.exec(detail);
  if (grep && !mcp) {
    return {
      kind: 'search',
      title: null,
      detail: `«${grep[1]}»`,
      fact: foundFact(entry, grep[2]),
      command: null,
    };
  }
  if (/^(grep|glob|search|ls|find)/i.test(name) && !mcp) {
    return { kind: 'search', title: null, detail, fact: foundFact(entry, undefined), command: null };
  }
  if (mcp) {
    const rest = detail.startsWith(entry.tool) ? detail.slice(entry.tool.length).trim() : detail;
    return {
      kind: 'mcp',
      title: `${mcpServerName(mcp[1])} · ${mcpToolName(mcp[2])}`,
      detail: rest,
      fact: null,
      command: null,
    };
  }
  if (/^(web|fetch)/i.test(name)) {
    const rest = detail.replace(/^(WebFetch|WebSearch|web_search)\s*/, '');
    return { kind: 'web', title: null, detail: rest, fact: null, command: null };
  }
  return { kind: 'other', title: null, detail, fact: null, command: null };
};

/** The words at the right end of a step's line: how long it runs or ran, and how it failed. */
export const stepMeta = (entry: ToolEntry, now: number): { text: string; failed: boolean } | null => {
  if (entry.ok === null) {
    const ms = now - entry.at;
    return { text: ms >= SHOW_DURATION_FROM_MS ? elapsed(ms) : t('next.tool.running'), failed: false };
  }
  const parts: string[] = [];
  const failed = entry.ok === false;
  if (failed) {
    const exit = entry.call?.exit;
    parts.push(
      exit !== undefined && exit !== 0 ? t('next.tool.exit', { code: exit }) : t('next.tool.failed'),
    );
  }
  const ms = entry.call?.durationMs;
  if (ms !== undefined && ms >= SHOW_DURATION_FROM_MS) parts.push(elapsed(ms));
  return parts.length === 0 ? null : { text: parts.join(' · '), failed };
};

/** The lines of output a folded step shows under its line: how a command ended. */
export interface IStepPreview {
  lines: string[];
  /** How many lines of the output are not among them. */
  more: number;
  failed: boolean;
}

const PREVIEW_LINES = 3;
const LEGACY_LINES_RE = /^\d+ lines$/;
const EXIT_LINE_RE = /^Exit code -?\d+$/;
const NO_OUTPUT_RE = /^\((?:Bash completed with )?no output\)$/i;

const meaningful = (text: string): string[] =>
  text
    .split('\n')
    .map((line) => line.replace(/\s+$/, ''))
    .filter(
      (line) => line.trim() !== '' && !EXIT_LINE_RE.test(line.trim()) && !NO_OUTPUT_RE.test(line.trim()),
    );

/** The last meaningful lines of a finished command (the first line of its error when it failed); null when none. */
export const stepPreview = (entry: ToolEntry, kind: TStepKind): IStepPreview | null => {
  if (kind !== 'run' || entry.ok === null) return null;
  const text = entry.output?.text ?? '';
  if (LEGACY_LINES_RE.test(text.trim()) || text.trim() === 'error') return null;
  const all = meaningful(text);
  const total = entry.call?.lines ?? all.length;
  if (entry.ok === false) {
    const first = all[0];
    if (!first) return null;
    return { lines: [first], more: Math.max(0, total - 1), failed: true };
  }
  const tail = entry.call?.tail ? meaningful(entry.call.tail) : all.slice(-PREVIEW_LINES);
  const lines = tail.slice(-PREVIEW_LINES);
  if (lines.length === 0) return null;
  return { lines, more: Math.max(0, total - lines.length), failed: false };
};

/** The head of «K actions»: what the run is (commands, files read), the step running now, how many failed. */
export const groupLabel = (tools: ToolEntry[], lineOf: (entry: ToolEntry) => IStepLine): string => {
  const lines = tools.map(lineOf);
  const kinds = new Set(lines.map((line) => line.kind));
  const [only] = kinds;
  let head = plural(tools.length, 'next.work.actions');
  if (kinds.size === 1 && only === 'run') head = plural(tools.length, 'next.work.commands');
  if (kinds.size === 1 && only === 'read') {
    head = plural(new Set(lines.map((line) => line.detail)).size, 'next.work.reads');
  }
  if (kinds.size === 1 && (only === 'edit' || only === 'create'))
    head = plural(tools.length, 'next.work.edits');
  if (kinds.size === 1 && only === 'search') head = plural(tools.length, 'next.work.searches');
  const parts = [head];
  const runningAt = tools.findIndex((tool) => tool.ok === null);
  if (runningAt >= 0) {
    const line = lines[runningAt];
    parts.push(line.title ?? line.detail ?? t('next.tool.running'));
  }
  const failed = tools.filter((tool) => tool.ok === false).length;
  if (failed > 0) parts.push(plural(failed, 'next.work.failedActions'));
  return parts.join(' · ');
};
