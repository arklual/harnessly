import { stripControlChars } from '@/lib/terminal-safe-text';
import { MODEL_ID_RE } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

export const MAX_QUESTION_CHARS = 4000;
export const MAX_EXCERPT_CHARS = 2000;
export const MAX_EXCERPTS = 5;
export const MAX_HISTORY_TURNS = 6;
const MAX_HISTORY_QUESTION_CHARS = MAX_QUESTION_CHARS;
const MAX_HISTORY_ANSWER_CHARS = 20_000;
const PROMPT_ANSWER_CHARS = 3000;
const TAB_ID_RE = /^[A-Za-z0-9._-]{1,128}$/;

export const SIDE_CHAT_ENDPOINT = '/api/platform/side-chat';

export type TSideChatRole = 'agent' | 'user';

export interface ISideChatExcerpt {
  quote: string;
  source?: { entryId: string; role: TSideChatRole };
}

export interface ISideChatTurn {
  question: string;
  answer: string;
}

export interface ISideChatRequest {
  tabId: string;
  question: string;
  excerpts: ISideChatExcerpt[];
  history: ISideChatTurn[];
  model: string | undefined;
}

export type TSideChatErrorCode =
  'unsupported' | 'no-session' | 'busy' | 'failed' | 'timeout' | 'cancelled' | 'unavailable';

export type TSideChatEvent =
  | { type: 'delta'; text: string }
  | { type: 'done'; text: string; costUsd?: number }
  | { type: 'error'; code: TSideChatErrorCode; message: string };

export type TSideChatParse = { ok: true; value: ISideChatRequest } | { ok: false; error: string };

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const fail = (error: string): TSideChatParse => ({ ok: false, error });

const parseExcerpt = (raw: unknown): ISideChatExcerpt | string => {
  if (!isRecord(raw) || typeof raw.quote !== 'string') return t('lib.hlSideChat.badExcerpt');
  const quote = stripControlChars(raw.quote).trim();
  if (!quote) return t('lib.hlSideChat.emptyExcerpt');
  if (quote.length > MAX_EXCERPT_CHARS) return t('lib.hlSideChat.excerptTooLong', { max: MAX_EXCERPT_CHARS });
  const source = raw.source;
  if (source === undefined) return { quote, source: undefined };
  if (
    !isRecord(source) ||
    typeof source.entryId !== 'string' ||
    (source.role !== 'agent' && source.role !== 'user')
  )
    return t('lib.hlSideChat.badExcerptSource');
  return { quote, source: { entryId: stripControlChars(source.entryId).slice(0, 200), role: source.role } };
};

const parseTurn = (raw: unknown): ISideChatTurn | null => {
  if (!isRecord(raw) || typeof raw.question !== 'string' || typeof raw.answer !== 'string') return null;
  const question = stripControlChars(raw.question).trim();
  const answer = stripControlChars(raw.answer).trim();
  if (!question || question.length > MAX_HISTORY_QUESTION_CHARS || answer.length > MAX_HISTORY_ANSWER_CHARS)
    return null;
  return { question, answer };
};

/** A body from the browser, checked and cleaned. The session is not part of it: the server finds it from the tab. */
export const parseSideChatRequest = (body: unknown): TSideChatParse => {
  if (!isRecord(body)) return fail(t('lib.hlSideChat.needJson'));
  const { tabId, question: rawQuestion, excerpts: rawExcerpts, history: rawHistory, model } = body;
  if (typeof tabId !== 'string' || !TAB_ID_RE.test(tabId)) return fail(t('lib.hlSideChat.needTabId'));
  const question = typeof rawQuestion === 'string' ? stripControlChars(rawQuestion).trim() : '';
  if (!question) return fail(t('lib.hlSideChat.emptyQuestion'));
  if (question.length > MAX_QUESTION_CHARS)
    return fail(t('lib.hlSideChat.questionTooLong', { max: MAX_QUESTION_CHARS }));
  if (model !== undefined && (typeof model !== 'string' || !MODEL_ID_RE.test(model)))
    return fail(t('lib.hlSideChat.unknownModel'));

  if (rawExcerpts !== undefined && (!Array.isArray(rawExcerpts) || rawExcerpts.length > MAX_EXCERPTS))
    return fail(t('lib.hlSideChat.tooManyExcerpts', { max: MAX_EXCERPTS }));
  const excerpts: ISideChatExcerpt[] = [];
  for (const raw of rawExcerpts ?? []) {
    const excerpt = parseExcerpt(raw);
    if (typeof excerpt === 'string') return fail(excerpt);
    excerpts.push(excerpt);
  }

  if (rawHistory !== undefined && !Array.isArray(rawHistory)) return fail(t('lib.hlSideChat.badHistory'));
  const history: ISideChatTurn[] = [];
  for (const raw of (rawHistory ?? []).slice(-MAX_HISTORY_TURNS)) {
    const turn = parseTurn(raw);
    if (!turn) return fail(t('lib.hlSideChat.badHistory'));
    history.push(turn);
  }

  return { ok: true, value: { tabId, question, excerpts, history, model } };
};

const PROMPT_TAGS = /<(\/?)(excerpt|turn|question|answer|side-chat-history)/gi;

/** Free text stays readable; only a tag that would end a block early or open another one is broken up. */
const keepInside = (text: string): string => text.replace(PROMPT_TAGS, '<\\$1$2');

const clipAnswer = (answer: string): string =>
  answer.length > PROMPT_ANSWER_CHARS ? `${answer.slice(0, PROMPT_ANSWER_CHARS).trimEnd()}…` : answer;

const excerptBlock = ({ quote, source }: ISideChatExcerpt): string =>
  `<excerpt${source ? ` from="${source.role} message"` : ''}>${keepInside(quote)}</excerpt>`;

const turnBlock = ({ question, answer }: ISideChatTurn): string =>
  `<turn><question>${keepInside(question)}</question><answer>${keepInside(clipAnswer(answer))}</answer></turn>`;

const PREAMBLE = [
  'This is a side question about the session above.',
  'Answer from the context you already have. No tools are available, and nothing said here is added to the session.',
  'Be brief and answer in the language of the question.',
].join(' ');

/** The one text sent to the CLI. It begins with a fixed sentence, so a question that starts with a dash is never an option. */
export const buildSideChatPrompt = ({ question, excerpts, history }: ISideChatRequest): string => {
  const parts = [PREAMBLE];
  if (history.length)
    parts.push(`<side-chat-history>\n${history.map(turnBlock).join('\n')}\n</side-chat-history>`);
  if (excerpts.length) parts.push(excerpts.map(excerptBlock).join('\n'));
  parts.push(keepInside(question));
  return parts.join('\n\n');
};

export const formatSideChatEvent = (event: TSideChatEvent): string => `data: ${JSON.stringify(event)}\n\n`;

const isEvent = (value: unknown): value is TSideChatEvent =>
  isRecord(value) &&
  ((value.type === 'delta' && typeof value.text === 'string') ||
    (value.type === 'done' && typeof value.text === 'string') ||
    (value.type === 'error' && typeof value.code === 'string' && typeof value.message === 'string'));

/** Frames of a server-sent stream read so far, and the unfinished tail to put in front of the next chunk. */
export const parseSideChatStream = (text: string): { events: TSideChatEvent[]; rest: string } => {
  const frames = text.split('\n\n');
  const rest = frames.pop() ?? '';
  const events: TSideChatEvent[] = [];
  for (const frame of frames) {
    if (!frame.startsWith('data: ')) continue;
    try {
      const parsed: unknown = JSON.parse(frame.slice(6));
      if (isEvent(parsed)) events.push(parsed);
    } catch {
      continue;
    }
  }
  return { events, rest };
};
