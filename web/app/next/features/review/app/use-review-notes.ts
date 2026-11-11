// The person's remarks on a chat's work, as the hub keeps them: read once per chat, changed only when the hub said yes
// (the screen shows what is kept, never a hope), and sent to the chat's agent as one message of its queue.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { sendMessage } from '@/next/commands/messages';
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { gatewayPath } from '@/next/core/net/machine';
import {
  agentMessage,
  draftsOf,
  NOTE_STATE,
  questionMessage,
  type IReviewNote,
  type TNoteState,
} from '@/next/features/review/app/notes';
import { LINE_SIDE } from '@/next/features/review/model';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

const TIMEOUT_MS = 15_000;

export type TNotesState = { k: 'loading' } | { k: 'failed' } | { k: 'ready'; notes: readonly IReviewNote[] };

const STATES = new Set<string>(Object.values(NOTE_STATE));
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const isStr = (v: unknown): v is string => typeof v === 'string';

const readNote = (v: unknown): IReviewNote | null => {
  if (!v || typeof v !== 'object') return null;
  const n = v as Record<string, unknown>;
  if (!isStr(n.id) || !isStr(n.path) || !isStr(n.lines) || !isStr(n.text)) return null;
  if (n.side !== LINE_SIDE.after && n.side !== LINE_SIDE.before) return null;
  if (!isNum(n.from) || !isNum(n.to) || !isStr(n.state) || !STATES.has(n.state)) return null;
  return {
    id: n.id,
    path: n.path,
    side: n.side,
    from: n.from,
    to: n.to,
    lines: n.lines,
    text: n.text,
    state: n.state as TNoteState,
    createdAt: isNum(n.createdAt) ? n.createdAt : 0,
    updatedAt: isNum(n.updatedAt) ? n.updatedAt : 0,
    ...(isStr(n.worktree) ? { worktree: n.worktree } : {}),
    ...(isStr(n.base) ? { base: n.base } : {}),
    ...(isNum(n.sentAt) ? { sentAt: n.sentAt } : {}),
  };
};

const readList = (v: unknown): IReviewNote[] => {
  const notes = (v as { notes?: unknown } | null)?.notes;
  if (!Array.isArray(notes)) throw new Error('review notes: no list');
  return notes.map(readNote).filter((n): n is IReviewNote => n !== null);
};

const readOne = (v: unknown): IReviewNote => {
  const note = readNote((v as { note?: unknown } | null)?.note);
  if (!note) throw new Error('review notes: bad note');
  return note;
};

const listPath = (conv: ConvId) =>
  gatewayPath(`/history/conversations/${encodeURIComponent(conv)}/review-notes`);
const notePath = (conv: ConvId, id: string) => `${listPath(conv)}/${encodeURIComponent(id)}`;

const words = () => ({
  intro: t('next.reviewApp.notes.msgIntro'),
  more: t('next.reviewApp.notes.msgMore'),
  before: t('next.reviewApp.notes.msgBefore'),
  question: t('next.reviewApp.notes.msgQuestion'),
});

export interface IReviewNotesApi {
  state: TNotesState;
  /** Keeps a note, new or changed; false when the hub did not. */
  save: (note: IReviewNote) => Promise<boolean>;
  remove: (id: string) => Promise<boolean>;
  /** Sends the drafts and the person's last words to the agent as one message, then marks the drafts sent. */
  send: (extra: string) => Promise<boolean>;
  /** A question on lines, to the agent at once; nothing is kept. */
  ask: (at: Pick<IReviewNote, 'path' | 'side' | 'from' | 'to' | 'lines'>, text: string) => void;
  retry: () => void;
}

export const useReviewNotes = (machine: MachineId, conv: ConvId): IReviewNotesApi => {
  const [loaded, setLoaded] = useState<{ conv: ConvId; state: TNotesState } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    void getPage(listPath(conv), readList, TIMEOUT_MS).then((res) => {
      if (!live) return;
      setLoaded({ conv, state: 'ok' in res ? { k: 'ready', notes: res.value } : { k: 'failed' } });
    });
    return () => {
      live = false;
    };
  }, [conv, attempt]);
  const state = useMemo<TNotesState>(
    () => (loaded?.conv === conv ? loaded.state : { k: 'loading' }),
    [loaded, conv],
  );

  const put = useCallback(
    (kept: IReviewNote) =>
      setLoaded((have) => {
        if (have?.conv !== conv || have.state.k !== 'ready') return have;
        const rest = have.state.notes.filter((n) => n.id !== kept.id);
        return { conv, state: { k: 'ready', notes: [...rest, kept] } };
      }),
    [conv],
  );

  const save = useCallback(
    async (note: IReviewNote) => {
      const res = await sendBody('PUT', notePath(conv, note.id), note, readOne, TIMEOUT_MS);
      if (!('ok' in res)) return false;
      put(res.value);
      return true;
    },
    [conv, put],
  );

  const remove = useCallback(
    async (id: string) => {
      const res = await sendPage('DELETE', notePath(conv, id), () => null, TIMEOUT_MS);
      if (!('ok' in res)) return false;
      setLoaded((have) => {
        if (have?.conv !== conv || have.state.k !== 'ready') return have;
        return { conv, state: { k: 'ready', notes: have.state.notes.filter((n) => n.id !== id) } };
      });
      return true;
    },
    [conv],
  );

  const send = useCallback(
    async (extra: string) => {
      if (state.k !== 'ready') return false;
      const drafts = draftsOf(state.notes);
      if (drafts.length === 0 && !extra.trim()) return false;
      sendMessage(machine, { conversationId: conv, text: agentMessage(drafts, extra, words()) });
      const now = Date.now();
      const marked = await Promise.all(
        drafts.map((n) => save({ ...n, state: NOTE_STATE.sent, sentAt: now })),
      );
      return marked.every(Boolean);
    },
    [state, machine, conv, save],
  );

  const ask = useCallback(
    (at: Pick<IReviewNote, 'path' | 'side' | 'from' | 'to' | 'lines'>, text: string) => {
      sendMessage(machine, { conversationId: conv, text: questionMessage(at, text, words()) });
    },
    [machine, conv],
  );

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return { state, save, remove, send, ask, retry };
};
