import { MAX_INITIAL_PROMPT_LENGTH } from '@/lib/hl-initial-prompt';
import { t } from '@/lib/i18n';
import { stripControlChars } from '@/lib/terminal-safe-text';

/**
 * A conversation carried on by a new agent on another machine. Nothing of the history moves: the agent of the first
 * conversation writes a handoff note (a fork of its session answers, the conversation itself does not change), and the
 * new conversation starts from that note alone.
 */

/** What the agent of the conversation is asked; it answers from the context it has, without tools. */
export const HANDOFF_QUESTION = [
  t('lib.hlHandoff.questionIntro'),
  t('lib.hlHandoff.questionTask'),
  '',
  t('lib.hlHandoff.questionGoal'),
  t('lib.hlHandoff.questionDone'),
  t('lib.hlHandoff.questionDecisions'),
  t('lib.hlHandoff.questionLeft'),
  t('lib.hlHandoff.questionTraps'),
  t('lib.hlHandoff.questionLocal'),
  '',
  t('lib.hlHandoff.questionSecrets'),
].join('\n');

/** The note takes most of the first message; the rest is the frame around it. */
export const HANDOFF_NOTE_MAX = MAX_INITIAL_PROMPT_LENGTH - 2_000;

const plain = (value: string): string => stripControlChars(value).replace(/\s+/g, ' ').trim();

export interface IHandoffSource {
  title: string;
  machineName: string;
  /** The link the first conversation opens by. */
  url: string;
  /** The handoff note its agent wrote. */
  note: string;
}

/** The first message of the new conversation. */
export const buildHandoffBrief = ({ title, machineName, url, note }: IHandoffSource): string => {
  const body = note.trim();
  const kept =
    body.length > HANDOFF_NOTE_MAX
      ? `${body.slice(0, HANDOFF_NOTE_MAX).trimEnd()}\n\n${t('lib.hlHandoff.noteCut', { max: HANDOFF_NOTE_MAX })}`
      : body;
  return [
    t('lib.hlHandoff.briefIntro', {
      title: plain(title).slice(0, 200),
      machine: plain(machineName).slice(0, 80),
      url,
    }),
    t('lib.hlHandoff.briefNoHistory'),
    t('lib.hlHandoff.briefSharedFolder'),
    t('lib.hlHandoff.briefFirst'),
    '',
    t('lib.hlHandoff.briefNoteHeader'),
    kept,
  ].join('\n');
};
