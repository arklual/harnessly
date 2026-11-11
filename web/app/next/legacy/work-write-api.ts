// The person's own hand in the review of a chat's work, over the runtime of the machine the worktree is on
// (cmd/runtime/routes_work_write.go): a file written from the review, and the changed files committed. And the chat's
// answers, read from the hub's copy of the chat, for «Пройти ревью».
import { scopedAppPath } from '@/lib/app-path';
import { WordedError } from '@/next/core/net/worded';
import type { IAgentAnswer } from '@/next/features/review/app/use-agent-review';
import { loadCentralChat } from '@/next/stores/central';
import { askSideChat } from '@/next/features/pane/side-chat-api';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import type { FileContent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

/** The revision the runtime checks a write against: sha256 of the text, hex. */
export const contentHash = async (text: string): Promise<string> => {
  const sum = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text));
  return [...new Uint8Array(sum)].map((b) => b.toString(16).padStart(2, '0')).join('');
};

const call = async (machineId: string | undefined, path: string, method: string, body: unknown) => {
  const address = `/api/platform${path}`;
  try {
    return await fetch(scopedAppPath(address, machineId), {
      method,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch {
    throw new WordedError(t('next.reviewApp.offlineText'), 'network');
  }
};

const said = async (res: Response): Promise<Record<string, unknown>> =>
  ((await res.json().catch(() => null)) as Record<string, unknown> | null) ?? {};

export interface IWorkWriter {
  /** Writes the text over the revision that was read; refused as a conflict when the disk changed since. With `over`,
   *  over whatever the disk has now. */
  save: (path: string, text: string, revision: string, over: boolean) => Promise<FileContent>;
  /** Commits the files; answers the new commit. */
  commit: (message: string, paths: readonly string[]) => Promise<string>;
}

/** The writes of a chat's worktree; the runtime takes only a worktree the chat (or its delegated chats) worked in. */
export const workWriter = (conv: string, root: string, machineId?: string): IWorkWriter => {
  const write = (path: string, text: string, expect: string) =>
    call(machineId, '/work/file', 'PUT', { conversationId: conv, root, path, text, expect });
  return {
    save: async (path, text, revision, over) => {
      let res = await write(path, text, revision);
      if (res.status === 409 && over) {
        const now = await said(res);
        if (typeof now.hash === 'string') res = await write(path, text, now.hash);
      }
      if (res.status === 409) throw new WordedError(t('next.reviewApp.edit.conflict'), 'conflict');
      const body = await said(res);
      if (!res.ok || typeof body.hash !== 'string')
        throw new WordedError(
          typeof body.error === 'string' ? body.error : t('next.reviewApp.httpError', { status: res.status }),
        );
      return { path, content: text, revision: body.hash, bytes: new TextEncoder().encode(text).length };
    },
    commit: async (message, paths) => {
      const res = await call(machineId, '/work/commit', 'POST', {
        conversationId: conv,
        root,
        message,
        paths,
      });
      const body = await said(res);
      if (res.status === 409) throw new WordedError(t('next.reviewApp.edit.nothing'), 'conflict');
      if (!res.ok || typeof body.sha !== 'string')
        throw new WordedError(
          typeof body.error === 'string' ? body.error : t('next.reviewApp.httpError', { status: res.status }),
        );
      return body.sha;
    },
  };
};

/** The agent's answers in a chat, from the hub's copy of it: its newest turns, oldest first. */
export const chatAnswers = async (conv: string): Promise<IAgentAnswer[] | null> => {
  const page = await loadCentralChat(conv);
  if (!('ok' in page)) return null;
  return page.value.entries
    .flatMap((e) => (e.t === 'say' ? [{ at: e.at, text: e.md }] : []))
    .sort((a, b) => a.at - b.at);
};

/** A question to the chat's side chat (a fork of its session, never kept): its whole answer, or the failure's words. */
export const sideAnswer = (
  machine: MachineId,
  conv: ConvId,
  question: string,
  signal: AbortSignal,
): Promise<string> =>
  new Promise((resolve, reject) => {
    void askSideChat(machine, conv, { question, history: [] }, signal, (event) => {
      if (event.type === 'done') resolve(event.text);
      else if (event.type === 'error') reject(new Error(event.message || event.code));
    }).then(() => {
      if (signal.aborted) reject(new Error('aborted'));
    });
  });
