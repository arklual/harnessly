import type { ITimelineEntry, TToolStatus } from '@/types/timeline';
import type { IRedactionReport, IRedactor } from '@/lib/hl-share-redact';
import { t } from '@/lib/i18n';

/**
 * Conversation snapshot: a whitelist projection of the timeline. Tool results, images,
 * attachments, approval prompts and command output never get in; thinking and diffs only when asked for.
 */

export const SHARE_TOOLS = {
  summary: 'summary',
  none: 'none',
} as const;

export type TShareTools = (typeof SHARE_TOOLS)[keyof typeof SHARE_TOOLS];

export interface IShareOptions {
  tools: TShareTools;
  thinking: boolean;
  diffs: boolean;
  economics: boolean;
}

export const DEFAULT_SHARE_OPTIONS: IShareOptions = {
  tools: 'summary',
  thinking: false,
  diffs: false,
  economics: true,
};

export const SHARE_LIMITS = {
  entryChars: 20_000,
  snapshotBytes: 5 * 1024 * 1024,
  actionSummaryChars: 400,
  noteChars: 600,
} as const;

export type TShareProvider = 'claude' | 'codex';

export type TShareDiff =
  { kind: 'replace'; file: string; old: string; new: string } | { kind: 'patch'; text: string };

export type TShareEntry =
  | { t: 'user'; at: number; text: string; images: number }
  | { t: 'assistant'; at: number; markdown: string; model: string | null }
  | { t: 'thinking'; at: number; text: string }
  | { t: 'action'; at: number; name: string; summary: string; status: TToolStatus; diff?: TShareDiff }
  | { t: 'plan'; at: number; markdown: string }
  | { t: 'note'; at: number; text: string };

export interface IShareTokens {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface IShareEconomics {
  tokens: IShareTokens;
  /** Sum of turn durations from the transcript; null when the CLI did not write them. */
  activeMs: number | null;
  wallMs: number;
  turns: number;
}

export interface IShareSnapshot {
  schema: 1;
  title: string;
  provider: TShareProvider;
  model: string | null;
  /** Teleport identity of the person who took the snapshot; null on a password-only runtime. */
  owner: { sub: string | null };
  capturedAt: number;
  firstAt: number | null;
  lastAt: number | null;
  options: IShareOptions;
  economics: IShareEconomics | null;
  entries: TShareEntry[];
}

export interface IShareReport extends IRedactionReport {
  entries: number;
  bytes: number;
  truncated: boolean;
}

export const parseShareOptions = (value: unknown): IShareOptions | null => {
  if (!value || typeof value !== 'object') return null;
  const { tools, thinking, diffs, economics } = value as Record<string, unknown>;
  if (tools !== SHARE_TOOLS.summary && tools !== SHARE_TOOLS.none) return null;
  if (typeof thinking !== 'boolean' || typeof diffs !== 'boolean' || typeof economics !== 'boolean')
    return null;
  return { tools, thinking, diffs, economics };
};

const DROPPED_BLOCKS =
  /<(system-reminder|cc-memory|local-command-stdout|local-command-stderr|local-command-caveat)\b[^>]*>[\s\S]*?<\/\1>/g;
const WRAPPER_TAG = /<\/?(pasted_content|cross-session-message|command-[a-z-]+)\b[^>]*>/g;

const cleanUserText = (text: string) => text.replace(DROPPED_BLOCKS, '').replace(WRAPPER_TAG, '').trim();

const oneLine = (text: string, max: number) => {
  const line = text.replace(/\s+/g, ' ').trim();
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
};

const TASK_STATUS: Record<string, TToolStatus> = { completed: 'success', failed: 'error' };

/** Timeline entries in, share entries out. Pure; redaction and limits come after. */
export const projectTimeline = (
  timeline: readonly ITimelineEntry[],
  options: IShareOptions,
): TShareEntry[] => {
  const result: TShareEntry[] = [];
  const withTools = options.tools === SHARE_TOOLS.summary;
  const action = (at: number, name: string, summary: string, status: TToolStatus, diff?: TShareDiff) => {
    if (!withTools) return;
    result.push({
      t: 'action',
      at,
      name,
      summary: oneLine(summary, SHARE_LIMITS.actionSummaryChars),
      status,
      ...(diff ? { diff } : {}),
    });
  };
  const note = (at: number, text: string) =>
    result.push({ t: 'note', at, text: oneLine(text, SHARE_LIMITS.noteChars) });

  for (const entry of timeline) {
    const at = entry.timestamp;
    switch (entry.type) {
      case 'user-message': {
        if (entry.pending || entry.attachmentPlaceholder) break;
        const text = cleanUserText(entry.text);
        const images = entry.images?.length ?? 0;
        if (text || images > 0) result.push({ t: 'user', at, text, images });
        break;
      }
      case 'assistant-message':
        if (entry.markdown.trim())
          result.push({ t: 'assistant', at, markdown: entry.markdown, model: entry.model ?? null });
        break;
      case 'thinking':
        if (options.thinking && entry.thinking.trim())
          result.push({ t: 'thinking', at, text: entry.thinking });
        break;
      case 'reasoning-summary':
        if (options.thinking && entry.summary.length > 0)
          result.push({ t: 'thinking', at, text: entry.summary.join('\n\n') });
        break;
      case 'tool-call': {
        const diff: TShareDiff | undefined =
          options.diffs && entry.diff
            ? {
                kind: 'replace',
                file: entry.diff.filePath,
                old: entry.diff.oldString,
                new: entry.diff.newString,
              }
            : undefined;
        action(at, entry.toolName, entry.summary, entry.status, diff);
        break;
      }
      case 'agent-group':
        action(at, 'Agent', [entry.agentType, entry.description].filter(Boolean).join(': '), 'success');
        break;
      case 'task-notification':
        action(at, 'Task', entry.summary, TASK_STATUS[entry.status] ?? 'pending');
        break;
      case 'exec-command-stream':
        action(at, 'exec', `$ ${entry.command}`, entry.status);
        break;
      case 'web-search':
        action(at, 'WebSearch', entry.query ?? '', entry.status);
        break;
      case 'mcp-tool-call':
        action(at, `${entry.server}.${entry.tool}`, entry.argumentsSummary ?? '', entry.status);
        break;
      case 'patch-apply': {
        const diff: TShareDiff | undefined =
          options.diffs && entry.diff ? { kind: 'patch', text: entry.diff } : undefined;
        action(at, 'apply_patch', entry.files.map((file) => file.path).join(', '), entry.status, diff);
        break;
      }
      case 'plan':
        if (entry.markdown.trim()) result.push({ t: 'plan', at, markdown: entry.markdown });
        break;
      case 'ask-user-question': {
        const questions = entry.questions.map((item) => item.question).join(' · ');
        // The note texts below mirror internal/share/snapshot.go byte for byte in the runtime's language: the
        // runtime builds the stored snapshot, this projection must produce the same entries, so they are
        // protocol values, not page copy.
        note(
          at,
          entry.answer
            ? t('lib.hlShareSnapshot.noteQuestionAnswer', { questions, answer: entry.answer })
            : t('lib.hlShareSnapshot.noteQuestion', { questions }),
        );
        break;
      }
      case 'interrupt':
        note(at, t('lib.hlShareSnapshot.noteInterrupt'));
        break;
      case 'context-compacted':
        note(at, t('lib.hlShareSnapshot.noteCompacted'));
        break;
      case 'error-notice':
        if (entry.severity === 'error')
          note(at, t('lib.hlShareSnapshot.noteError', { message: entry.message }));
        break;
      case 'slash-command':
        if (!entry.pending) note(at, `/${entry.name}${entry.args ? ` ${entry.args}` : ''}`);
        break;
      default:
        // tool-result, approval-request, command-output, task-progress, turn-end, session-exit and any type added later stay out.
        break;
    }
  }
  return result;
};

const mapEntryStrings = (entry: TShareEntry, map: (value: string) => string): TShareEntry => {
  switch (entry.t) {
    case 'user':
      return { ...entry, text: map(entry.text) };
    case 'assistant':
      return { ...entry, markdown: map(entry.markdown) };
    case 'thinking':
    case 'note':
      return { ...entry, text: map(entry.text) };
    case 'plan':
      return { ...entry, markdown: map(entry.markdown) };
    case 'action': {
      const diff =
        entry.diff &&
        (entry.diff.kind === 'patch'
          ? { kind: 'patch' as const, text: map(entry.diff.text) }
          : {
              kind: 'replace' as const,
              file: map(entry.diff.file),
              old: map(entry.diff.old),
              new: map(entry.diff.new),
            });
      return { ...entry, name: map(entry.name), summary: map(entry.summary), ...(diff ? { diff } : {}) };
    }
  }
};

/** Every string a viewer can see goes through the redactor: title, entries, diffs. Runs at capture and at every render. */
export const redactSnapshot = (snapshot: IShareSnapshot, redactor: IRedactor): IShareSnapshot => ({
  ...snapshot,
  title: redactor.redact(snapshot.title),
  model: snapshot.model === null ? null : redactor.redact(snapshot.model),
  entries: snapshot.entries.map((entry) => mapEntryStrings(entry, redactor.redact)),
});

/** Same mark as internal/share/snapshot.go (truncatedMark): a protocol value shared with the runtime. */
const truncatedMark = () => t('lib.hlShareSnapshot.truncatedMark');

const truncateEntries = (entries: readonly TShareEntry[]) => {
  let truncated = false;
  const cut = (value: string) => {
    if (value.length <= SHARE_LIMITS.entryChars) return value;
    truncated = true;
    return `${value.slice(0, SHARE_LIMITS.entryChars)}${truncatedMark()}`;
  };
  return { entries: entries.map((entry) => mapEntryStrings(entry, cut)), truncated: () => truncated };
};

export interface ICaptureInput {
  timeline: readonly ITimelineEntry[];
  provider: TShareProvider;
  title: string;
  owner: { sub: string | null };
  options: IShareOptions;
  economics: IShareEconomics | null;
  redactor: IRedactor;
  now: number;
}

export type TCaptureResult =
  { ok: true; snapshot: IShareSnapshot; report: IShareReport } | { ok: false; status: 413; bytes: number };

/** Projection → redaction → per-entry limit (after redaction, so a cut never splits a secret) → 5 MB check. */
export const captureSnapshot = (input: ICaptureInput): TCaptureResult => {
  const projected = projectTimeline(input.timeline, input.options);
  const lastModel = [...projected].reverse().find((entry) => entry.t === 'assistant' && entry.model);
  const draft: IShareSnapshot = {
    schema: 1,
    title: input.title,
    provider: input.provider,
    model: lastModel?.t === 'assistant' ? lastModel.model : null,
    owner: input.owner,
    capturedAt: input.now,
    firstAt: projected[0]?.at ?? null,
    lastAt: projected.at(-1)?.at ?? null,
    options: input.options,
    economics: input.options.economics ? input.economics : null,
    entries: projected,
  };
  const redacted = redactSnapshot(draft, input.redactor);
  const limited = truncateEntries(redacted.entries);
  const snapshot = { ...redacted, entries: limited.entries };
  const bytes = new TextEncoder().encode(JSON.stringify(snapshot)).length;
  if (bytes > SHARE_LIMITS.snapshotBytes) return { ok: false, status: 413, bytes };
  return {
    ok: true,
    snapshot,
    report: {
      ...input.redactor.report(),
      entries: snapshot.entries.length,
      bytes,
      truncated: limited.truncated(),
    },
  };
};

const num = (value: unknown) => (typeof value === 'number' && Number.isFinite(value) ? value : 0);

/**
 * Tokens and time straight from the raw transcript. Claude writes one line per content block with the same
 * message.id and usage, so usage is counted once per id; Codex token_count is cumulative, the last one wins.
 */
export const readTranscriptEconomics = (content: string, provider: TShareProvider): IShareEconomics => {
  const tokens: IShareTokens = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
  const seen = new Set<string>();
  let activeMs = 0;
  let hasDurations = false;
  let turns = 0;
  let firstAt: number | null = null;
  let lastAt: number | null = null;

  for (const line of content.split('\n')) {
    if (!line) continue;
    let item: Record<string, unknown>;
    try {
      item = JSON.parse(line) as Record<string, unknown>;
    } catch {
      continue;
    }
    const at = typeof item.timestamp === 'string' ? Date.parse(item.timestamp) : NaN;
    if (!Number.isNaN(at)) {
      firstAt ??= at;
      lastAt = at;
    }
    if (provider === 'claude') {
      if (item.isSidechain === true) continue;
      const message = item.message as { id?: string; usage?: Record<string, unknown> } | undefined;
      if (item.type === 'assistant' && message?.usage && message.id && !seen.has(message.id)) {
        seen.add(message.id);
        tokens.input += num(message.usage.input_tokens);
        tokens.output += num(message.usage.output_tokens);
        tokens.cacheRead += num(message.usage.cache_read_input_tokens);
        tokens.cacheWrite += num(message.usage.cache_creation_input_tokens);
      }
      if (item.type === 'system' && item.subtype === 'turn_duration') {
        hasDurations = true;
        turns += 1;
        activeMs += num(item.durationMs);
      }
      continue;
    }
    const payload = item.payload as Record<string, unknown> | undefined;
    if (item.type !== 'event_msg' || !payload) continue;
    if (payload.type === 'token_count') {
      const usage = (payload.info as { total_token_usage?: Record<string, unknown> } | null)
        ?.total_token_usage;
      if (usage) {
        const cached = num(usage.cached_input_tokens);
        tokens.input = Math.max(0, num(usage.input_tokens) - cached);
        tokens.cacheRead = cached;
        tokens.output = num(usage.output_tokens);
      }
    }
    if (payload.type === 'task_complete') {
      turns += 1;
      if (typeof payload.duration_ms === 'number') {
        hasDurations = true;
        activeMs += payload.duration_ms;
      }
    }
  }
  return {
    tokens,
    activeMs: hasDurations ? activeMs : null,
    wallMs: firstAt !== null && lastAt !== null ? lastAt - firstAt : 0,
    turns,
  };
};
