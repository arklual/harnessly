// Small pieces of logic behind the feed rows that are neither text nor tool lines.
import { t } from '@/lib/i18n';
import { parseAnswer } from '@/lib/ask-question';
import type { IAskUserQuestionItem } from '@/types/timeline';

/** The title of a plan: its first line with text, without the heading marks. */
export const planTitle = (markdown: string): string => {
  const line = markdown.split('\n').find((l) => l.replace(/^#+\s*/, '').trim() !== '');
  return line ? line.replace(/^#+\s*/, '').trim() : '';
};

/** What was answered to a question, in words: picked labels and the free text, or null while unanswered. */
export const answeredLabels = (
  question: IAskUserQuestionItem,
  answers: Record<string, string> | undefined,
): { picked: number[]; other: string } | null => {
  const answer = answers?.[question.question];
  if (answer === undefined) return null;
  return parseAnswer(answer, question.options);
};

/** A span of time in the shortest form: `420ms`, `3.2s`, `1m 05s`. */
export const formatDuration = (ms: number): string => {
  if (!Number.isFinite(ms) || ms < 0) return '';
  if (ms < 1000) return t('timeline.duration.ms', { ms: Math.round(ms) });
  const seconds = ms / 1000;
  if (seconds < 60) return t('timeline.duration.seconds', { s: seconds.toFixed(1) });
  return t('timeline.duration.minutes', {
    m: Math.floor(seconds / 60),
    s: String(Math.round(seconds % 60)).padStart(2, '0'),
  });
};

/** Token counts as `842` or `12.4k`. */
export const formatTokens = (n: number): string => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n));

/** How much of the context a compaction freed, in percent; null when either side is unknown or nothing was freed. */
export const compactionSaving = (before?: number, after?: number): number | null => {
  if (before === undefined || after === undefined || before <= 0 || after >= before) return null;
  return Math.round(((before - after) / before) * 100);
};

/** The longest message shown whole; past it (or with a line break) the notice folds. */
export const NOTICE_PREVIEW = 200;

export const noticePreview = (message: string, open: boolean): { text: string; long: boolean } => {
  const long = message.length > NOTICE_PREVIEW || message.includes('\n');
  return { long, text: long && !open ? `${message.slice(0, NOTICE_PREVIEW)}…` : message };
};

/** Codex marks what an edit does to a file with words of its own; reduce them to one of three. */
export const patchFileAction = (status?: string): 'create' | 'delete' | 'modify' | 'other' => {
  const s = (status ?? '').toLowerCase();
  if (s.includes('add') || s.includes('create')) return 'create';
  if (s.includes('delete') || s.includes('remove')) return 'delete';
  if (s.includes('update') || s.includes('modify')) return 'modify';
  return 'other';
};
