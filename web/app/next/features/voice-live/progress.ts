// What the agent does this moment, as a few words the voice says (asked for 15.11: «смотрю доску»,
// «перекрашиваю колонки»). Only steps that mean something to the person get words, and no tool names.
import type { Now } from '@/next/protocol/v2';

/** The voice says a step at most this often: more reads as chatter. */
export const PROGRESS_GAP_MS = 3500;

const BOARD_RE = /\.dc\.html$/i;

/** «доску «Оплата»», «файл README.md», or '' when the step names no file. */
const whatOf = (summary: string): string => {
  const name = summary.match(/([^/\\\n]+?\.(?:dc\.html|[a-z0-9]{1,5}))(?=$|[\s:,)])/iu)?.[1]?.trim() ?? '';
  if (!name) return '';
  if (BOARD_RE.test(name)) return `доску «${name.replace(BOARD_RE, '')}»`;
  return `файл ${name}`;
};

const READ = new Set(['read', 'grep', 'glob', 'ls', 'notebookread', 'view']);
const EDIT = new Set([
  'edit',
  'multiedit',
  'write',
  'notebookedit',
  'apply_patch',
  'str_replace_based_edit_tool',
]);
const RUN = new Set(['bash', 'shell', 'exec_command', 'local_shell']);
const WEB = new Set(['websearch', 'webfetch', 'web_search']);
const HELP = new Set(['task', 'agent']);
const PLAN = new Set(['todowrite', 'update_plan']);

/** The words for the agent's step, or null when it is not worth saying. */
export const progressPhrase = (now: Now | null): string | null => {
  const action = now?.action;
  if (!action) return null;
  const raw = action.tool.toLowerCase();
  const tool = raw.startsWith('mcp__') ? (raw.split('__').pop() ?? raw) : raw;
  const what = whatOf(action.summary);
  if (READ.has(tool)) return what ? `Смотрю ${what}` : 'Смотрю, что есть';
  if (EDIT.has(tool)) return what ? `Правлю ${what}` : 'Вношу правку';
  if (RUN.has(tool)) return 'Проверяю на машине';
  if (WEB.has(tool)) return 'Ищу в интернете';
  if (HELP.has(tool)) return 'Отдаю часть помощнику';
  if (PLAN.has(tool)) return 'Составляю план';
  if (/screenshot|snapshot|navigate|browser|check/.test(tool)) return 'Смотрю, как получилось';
  return null;
};

/**
 * Whether a phrase is said now: a new one, and the last said long enough ago. `last` is what was said and when.
 */
export const shouldSay = (
  phrase: string | null,
  last: { text: string; at: number } | null,
  at: number,
): boolean => phrase !== null && phrase !== last?.text && (last === null || at - last.at >= PROGRESS_GAP_MS);
