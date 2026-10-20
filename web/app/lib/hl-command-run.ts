import { BUILTIN_COMMANDS, isCommandName } from '@/lib/hl-command-catalog/items';
import type { ICommandItem, TCommandOutput } from '@/lib/hl-command-catalog/types';
import { parseChoiceOptions } from '@/lib/permission-prompt';
import { CLAUDE_EFFORTS, MODEL_ID_RE, type TAgentProvider } from '@/types/hl-launch';
import type { TCliState } from '@/types/timeline';
import { t } from '@/lib/i18n';

/**
 * Runs a slash line in an agent's tmux pane and reads what the TUI did with it.
 * The screen classifiers are pure (covered by tests/fixtures/tui); `runCommandLine` gets tmux through a driver.
 */

export const COMMAND_LINE_MAX = 2000;
const LINE_RE = /^\/([A-Za-z0-9][A-Za-z0-9._:-]{0,127})(?:[ \u00a0]+([\s\S]*))?$/;
// C0/C1 controls, DEL, Unicode line/paragraph separators and bidi overrides: none of them belongs in a typed command.
const FORBIDDEN_CHARS_RE = /[\u0000-\u001f\u007f-\u009f\u2028\u2029\u202a-\u202e\u2066-\u2069]/;
export const SESSION_NAME_RE = /^[A-Za-z0-9_-]{1,200}$/;

const POLL_MS = 75;
const POLL_LIMIT_MS = 2500;
/** A plain command is taken as sent once the TUI's input line is empty again and nothing opened for this long. */
const SETTLE_MS = 600;
const CONFIRM_SETTLE_MS = 1500;
const MENU_SCAN_LINES = 25;
const PANEL_LINES = 30;

export interface IRunLine {
  name: string;
  args: string;
}

export type TLineCheck = ({ ok: true } & IRunLine) | { ok: false; error: string };

/** One line, 2–2000 characters, no control characters, `/name` and optional arguments. */
export const parseRunLine = (line: unknown): TLineCheck => {
  if (typeof line !== 'string') return { ok: false, error: t('lib.hlCommandRun.needLine') };
  if (line.length < 2 || line.length > COMMAND_LINE_MAX)
    return { ok: false, error: t('lib.hlCommandRun.lineLength') };
  if (FORBIDDEN_CHARS_RE.test(line)) return { ok: false, error: t('lib.hlCommandRun.oneLine') };
  const match = line.match(LINE_RE);
  if (!match || !isCommandName(match[1])) return { ok: false, error: t('lib.hlCommandRun.startsWithSlash') };
  return { ok: true, name: match[1], args: (match[2] ?? '').trim() };
};

/** The built-in behind a name or alias; anything else (skills, plugins, typos) is typed as a prompt and the CLI answers. */
export const classifyCommand = (provider: TAgentProvider, name: string): ICommandItem | null => {
  const wanted = name.toLowerCase();
  return (
    BUILTIN_COMMANDS[provider].find((item) => item.name === wanted || item.aliases.includes(wanted)) ?? null
  );
};

// CSI, OSC and two-byte escapes.
const ANSI_RE = /\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g;
export const stripAnsi = (text: string): string => text.replace(ANSI_RE, '');

const tail = (lines: readonly string[], count: number) => lines.slice(Math.max(0, lines.length - count));
const trimEndLines = (screen: string): string[] => {
  const lines = screen.replace(/\r/g, '').split('\n');
  while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
  return lines;
};

// Claude 2.1.28x: «✽ Contemplating… (2s · ↓ 20 tokens)» while working, «✻ Baked for 5s» when done (no ellipsis).
const CLAUDE_SPINNER_RE = /^[·✢✳✶✻✽*]\s+\S+…/;

/**
 * The screen says the agent is working: Claude's spinner line, or «esc to interrupt» (Codex «Working (3s • esc to
 * interrupt)», older Claude). While Claude streams text the spinner can be off screen, so the hook-driven cliState
 * is checked too (`ICommandRunDriver.cliState`).
 */
export const isBusyScreen = (plain: string): boolean =>
  tail(trimEndLines(plain), 12).some(
    (line) => /esc to interrupt/i.test(line) || CLAUDE_SPINNER_RE.test(line),
  );

/** Like `isBusyScreen`, over the whole screen: a long todo list pushes the spinner above the last lines. */
export const isBusyAnywhere = (plain: string): boolean =>
  trimEndLines(plain).some((line) => /esc to interrupt/i.test(line) || CLAUDE_SPINNER_RE.test(line));

export interface ITuiMenu {
  title: string | null;
  request: string | null;
  options: string[];
  focusedIndex: number;
}

const SEPARATOR_RE = /^\s*[▔─━]+\s*$/;
const BOX_EDGE_RE = /^\s*[│╰╭╮╯┃]/;
const HEADING_LINES = 4;

/**
 * The lines just above a menu's first option, up to a rule, a box edge or unindented text: its title and question.
 * Claude separates them with blank lines («Switch model?» … «This conversation is cached…»), Codex does not.
 */
const menuHeading = (lines: readonly string[], firstOption: string): string[] => {
  const label = firstOption.replace(/^\d+\.\s*/, '');
  const start = lines.findIndex((line) => /\d+\.?\s*/.test(line) && line.includes(label));
  const heading: string[] = [];
  for (let i = start - 1; i >= 0 && heading.length < HEADING_LINES; i -= 1) {
    const line = lines[i];
    if (!line.trim()) continue;
    if (SEPARATOR_RE.test(line) || BOX_EDGE_RE.test(line) || !/^\s/.test(line)) break;
    heading.unshift(line.trim());
  }
  return heading;
};

/** A numbered choice block (with a focus marker) near the bottom of the screen. */
export const findMenu = (plain: string): ITuiMenu | null => {
  const lines = tail(trimEndLines(plain), MENU_SCAN_LINES);
  const parsed = parseChoiceOptions(lines.join('\n'));
  if (parsed.options.length < 2) return null;
  const heading = parsed.title
    ? parsed.title.split('\n').filter(Boolean)
    : menuHeading(lines, parsed.options[0]);
  return {
    title: heading[0] ?? null,
    request: heading.length > 1 ? heading.slice(1).join('\n') : null,
    options: parsed.options,
    focusedIndex: parsed.focusedIndex,
  };
};

// Footers of TUI panels and pickers. Case matters: Claude's «(shift+tab to cycle)» and «esc to interrupt» are not panels.
const PANEL_HINT_RE =
  /Esc to (?:cancel|close|exit|go back|dismiss)|esc to (?:close|go back|cancel|exit|dismiss)|Enter to (?:select|confirm|set|continue)|enter to (?:select|confirm|continue)|↑\/↓|←\/→|Tab to (?:switch|cycle)/;

export const countPanelHints = (plain: string): number =>
  tail(trimEndLines(plain), 20).filter((line) => PANEL_HINT_RE.test(line)).length;

const PANEL_TOP_RE = /^\s*▔{8,}\s*$/;

/**
 * The panel part of the screen as a snapshot: below Claude's `▔▔▔` top rule when there is one (history above it
 * is not the panel), at most the last 30 non-empty lines.
 */
export const panelSnapshot = (plain: string): string => {
  const lines = trimEndLines(plain);
  let top = -1;
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    if (PANEL_TOP_RE.test(lines[i])) {
      top = i;
      break;
    }
  }
  return tail(
    lines.slice(top + 1).filter((line) => line.trim() && !/^[▔─━\s]+$/.test(line)),
    PANEL_LINES,
  ).join('\n');
};

// SGR sequences; the `2` parameter is «dim», which both CLIs use for their input placeholder.
const SGR_RE = /^\x1b\[([0-9;]*)m/;
const INPUT_MARKER_RE = /^[❯›](?=[ \u00a0\x1b]|$)/;

/**
 * Text typed into the TUI's own input line: '' when it is empty (or shows its dim placeholder), null when no input
 * line is on screen. The input line is the last one starting at column 0 with `❯` (Claude) or `›` (Codex).
 */
export const readInputLine = (styled: string): string | null => {
  const lines = trimEndLines(styled);
  for (let i = lines.length - 1; i >= 0; i -= 1) {
    let rest = lines[i];
    // Leading attribute resets belong to the previous line's styling.
    while (SGR_RE.test(rest)) rest = rest.replace(SGR_RE, '');
    if (!INPUT_MARKER_RE.test(rest)) continue;
    rest = rest.slice(1);
    let dim = false;
    for (;;) {
      const sgr = rest.match(SGR_RE);
      if (sgr) {
        if (sgr[1].split(';').includes('2')) dim = true;
        else if (sgr[1] === '' || sgr[1].split(';').includes('0') || sgr[1].split(';').includes('22'))
          dim = false;
        rest = rest.slice(sgr[0].length);
        continue;
      }
      if (/^[ \u00a0]/.test(rest)) {
        rest = rest.slice(1);
        continue;
      }
      break;
    }
    const text = stripAnsi(rest)
      .replace(/\u00a0/g, ' ')
      .trim();
    return dim ? '' : text;
  }
  return null;
};

const CONFIRM_RE = /Switch model\?|Change effort level\?/;
const MODEL_SET_RE = /Set model to\b/g;
// `/effort ultracode` answers «Ultracode on (this session only): dynamic workflows on every task. Effort stays xhigh.»
// (Claude Code 2.1.296).
const EFFORT_SET_RE = /Set effort level to\b|Ultracode on\b/g;
const KEPT_RE = /Kept (?:model|effort)\b/g;
const countMatches = (text: string, re: RegExp) => text.match(re)?.length ?? 0;

export interface IScreenState {
  busy: boolean;
  menu: ITuiMenu | null;
  panel: boolean;
  input: string | null;
}

export const readScreen = (styled: string): IScreenState & { plain: string } => {
  const plain = stripAnsi(styled);
  return {
    plain,
    busy: isBusyScreen(plain),
    menu: findMenu(plain),
    panel: countPanelHints(plain) > 0,
    input: readInputLine(styled),
  };
};

/** Keys the mirror may press: menu navigation, never text. Digits pick a numbered option. */
export const MIRROR_KEYS = [
  'Up',
  'Down',
  'Left',
  'Right',
  'Tab',
  'BTab',
  'Enter',
  'Escape',
  'Space',
  '1',
  '2',
  '3',
  '4',
  '5',
  '6',
  '7',
  '8',
  '9',
] as const;
export type TMirrorKey = (typeof MIRROR_KEYS)[number];

/** What the mirror polls: GET/POST /api/platform/commands/screen. */
export interface IMirrorScreen {
  menu: ITuiMenu | null;
  panel: boolean;
  busy: boolean;
  /** The last lines of the screen, ANSI stripped. */
  screen: string;
}

export interface IApplied {
  model?: string;
  effort?: string;
}

export type TRunBody =
  | { outcome: 'sent'; expect: TCommandOutput; command: string }
  | { outcome: 'confirmed'; applied: IApplied; asked: boolean; command: string }
  | { outcome: 'menu'; menu: ITuiMenu; command: string }
  | { outcome: 'panel'; screen: string; command: string };

export const RUN_ERRORS = {
  invalid: 'invalid',
  notFound: 'not-found',
  blocked: 'blocked',
  notRunning: 'not-running',
  menuOpen: 'menu-open',
  busy: 'busy',
  inputNotEmpty: 'input-not-empty',
  noScreen: 'no-screen',
  declined: 'declined',
} as const;
export type TRunError = (typeof RUN_ERRORS)[keyof typeof RUN_ERRORS];

export type TRunResult =
  | { status: 200; body: TRunBody }
  | { status: 400 | 404 | 409 | 422 | 500; body: { error: TRunError; message: string } };

export interface ICommandRunDriver {
  hasSession: (session: string) => Promise<boolean>;
  /** Foreground process of the pane, e.g. `claude`, `node`, `zsh`. */
  currentCommand: (session: string) => Promise<string | null>;
  /** Visible screen with SGR attributes. */
  capture: (session: string) => Promise<string | null>;
  sendLine: (session: string, line: string) => Promise<void>;
  sendKey: (session: string, key: TMirrorKey) => Promise<void>;
  sleep: (ms: number) => Promise<void>;
  now: () => number;
  isShell: (command: string) => boolean;
  /** Hook-driven work state of the tab behind the session; null when the runtime does not track it. */
  cliState: (session: string) => Promise<TCliState | null>;
}

export interface IRunInput {
  session: string;
  provider: TAgentProvider;
  line: string;
  /** Only switch-model: refuse anything but `/model <alias>`. */
  expect?: 'model';
}

const fail = (status: 400 | 404 | 409 | 422 | 500, error: TRunError, message: string): TRunResult => ({
  status,
  body: { error, message },
});

/** `/model sonnet` and `/effort high` of Claude change the launch the conversation resumes with. */
export const launchChangeOf = (provider: TAgentProvider, line: IRunLine): IApplied | null => {
  if (provider !== 'claude' || !line.args || /\s/.test(line.args)) return null;
  const name = line.name.toLowerCase();
  const value = line.args.toLowerCase();
  if (name === 'model' && MODEL_ID_RE.test(value)) return { model: value };
  if (name === 'effort' && (CLAUDE_EFFORTS as readonly string[]).includes(value)) return { effort: value };
  return null;
};

/**
 * Check the line, refuse blocked commands and panes where the agent is not running, refuse when the TUI
 * shows a menu, is busy (prompt-like commands are queued by the CLI) or has typed text; type the line literally,
 * then poll the screen: confirm «Switch model?»/«Change effort level?» for `/model x`/`/effort x`, report a new
 * menu or panel, otherwise «sent».
 */
export const runCommandLine = async (input: IRunInput, driver: ICommandRunDriver): Promise<TRunResult> => {
  const parsed = parseRunLine(input.line);
  if (!parsed.ok) return fail(400, RUN_ERRORS.invalid, parsed.error);
  const command = `/${parsed.name}`;
  if (!SESSION_NAME_RE.test(input.session))
    return fail(400, RUN_ERRORS.invalid, t('lib.hlCommandRun.badSession'));

  const item = classifyCommand(input.provider, parsed.name);
  if (item?.execution === 'blocked') {
    return fail(
      422,
      RUN_ERRORS.blocked,
      t('lib.hlCommandRun.blocked', { command, reason: item.note ?? t('lib.hlCommandRun.blockedReason') }),
    );
  }
  const change = launchChangeOf(input.provider, parsed);
  if (input.expect === 'model' && !change?.model)
    return fail(400, RUN_ERRORS.invalid, t('lib.hlCommandRun.unknownModel'));

  if (!(await driver.hasSession(input.session)))
    return fail(404, RUN_ERRORS.notFound, t('lib.hlCommandRun.sessionNotFound'));
  const process = await driver.currentCommand(input.session);
  if (!process || driver.isShell(process)) {
    return fail(409, RUN_ERRORS.notRunning, t('lib.hlCommandRun.notRunning'));
  }

  const before = await driver.capture(input.session);
  if (before === null) return fail(500, RUN_ERRORS.noScreen, t('lib.hlCommandRun.noScreen'));
  const start = readScreen(before);
  if (start.menu) {
    return fail(409, RUN_ERRORS.menuOpen, t('lib.hlCommandRun.menuOpen'));
  }
  const queueable = item === null || (item.execution === 'send' && item.output === 'reply');
  const busy = start.busy || (await driver.cliState(input.session)) === 'busy';
  if (busy && !queueable) {
    return fail(409, RUN_ERRORS.busy, t('lib.hlCommandRun.busy'));
  }
  if (start.input) {
    return fail(409, RUN_ERRORS.inputNotEmpty, t('lib.hlCommandRun.inputNotEmpty'));
  }

  const line = parsed.args ? `${command} ${parsed.args}` : command;
  await driver.sendLine(input.session, line);

  const expectsMenu =
    item?.execution === 'menu' ||
    item?.execution === 'terminal' ||
    (item?.execution === 'model' && !parsed.args) ||
    (item?.execution === 'effort' && !parsed.args);
  const startedAt = driver.now();
  let confirmedAt: number | null = null;
  while (driver.now() - startedAt < POLL_LIMIT_MS + (confirmedAt === null ? 0 : CONFIRM_SETTLE_MS)) {
    await driver.sleep(POLL_MS);
    const styled = await driver.capture(input.session);
    if (styled === null) continue;
    const screen = readScreen(styled);
    const elapsed = driver.now() - startedAt;

    if (change && CONFIRM_RE.test(screen.plain) && screen.menu && confirmedAt === null) {
      // Picking the model in the web is the consent to re-read the history without cache; «1. Yes» is focused.
      if (screen.menu.focusedIndex === 0 && /^1\.\s*Yes/.test(screen.menu.options[0] ?? '')) {
        await driver.sendKey(input.session, 'Enter');
        confirmedAt = driver.now();
        continue;
      }
      return { status: 200, body: { outcome: 'menu', menu: screen.menu, command } };
    }
    const appliedRe = change?.model ? MODEL_SET_RE : EFFORT_SET_RE;
    if (
      change &&
      !CONFIRM_RE.test(screen.plain) &&
      countMatches(screen.plain, appliedRe) > countMatches(start.plain, appliedRe)
    ) {
      return {
        status: 200,
        body: { outcome: 'confirmed', applied: change, asked: confirmedAt !== null, command },
      };
    }
    if (
      change &&
      confirmedAt !== null &&
      countMatches(screen.plain, KEPT_RE) > countMatches(start.plain, KEPT_RE)
    ) {
      return fail(409, RUN_ERRORS.declined, t('lib.hlCommandRun.declined'));
    }
    if (confirmedAt !== null) continue;
    if (screen.menu) return { status: 200, body: { outcome: 'menu', menu: screen.menu, command } };
    if (screen.panel && countPanelHints(screen.plain) > countPanelHints(start.plain)) {
      return { status: 200, body: { outcome: 'panel', screen: panelSnapshot(screen.plain), command } };
    }
    if (!expectsMenu && !change && screen.input === '' && elapsed >= SETTLE_MS) break;
  }
  if (change && confirmedAt !== null)
    return { status: 200, body: { outcome: 'confirmed', applied: change, asked: true, command } };
  return { status: 200, body: { outcome: 'sent', expect: item?.output ?? 'reply', command } };
};
