// The shell's two dialogs over the machine: «Новый проект» (a name) and «Продолжить сессию» (what terminal agents kept
// there). The second asks the machine when it opens; a list that did not come says why.
import { useState } from 'react';
import { settled, pageText } from '@/next/bindings/outcome';
import { createProject, resumeSession } from '@/next/commands';
import { loadSessions } from '@/next/commands/pages';
import NewProjectDialog from '@/next/features/sidebar/new-project-dialog';
import ResumeDialog, { type IResumeSession } from '@/next/features/shell/resume-dialog';
import type { AgentSession, ConvId, MachineId } from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';

const sessionOf = (session: AgentSession, machine: string): IResumeSession => ({
  id: session.sessionId,
  provider: session.provider,
  first: session.title || t('next.shellData.untitled'),
  last: plural(session.turns, 'next.resume.turns'),
  dir: session.cwd,
  branch: null,
  machine,
  at: session.updatedAt,
  off: false,
});

export const useProjectDialog = (machine: MachineId | null) => {
  const [open, setOpen] = useState(false);

  const show = () => {
    if (machine) setOpen(true);
  };

  const dialog = machine && (
    <NewProjectDialog
      open={open}
      onClose={() => setOpen(false)}
      onCreate={async (name) => {
        await settled(createProject(machine, name));
      }}
    />
  );
  return { show, dialog };
};

export const useResumeDialog = (
  machine: MachineId | null,
  machineName: string,
  now: number,
  onOpened: (id: ConvId) => void,
) => {
  const [open, setOpen] = useState(false);
  const [sessions, setSessions] = useState<AgentSession[] | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  const ask = (m: MachineId) => {
    setSessions(null);
    setFailure(null);
    return loadSessions(m).then((res) => {
      if ('ok' in res) setSessions(res.value);
      else setFailure(pageText(res));
    });
  };

  const show = () => {
    if (!machine) return;
    setOpen(true);
    void ask(machine);
  };

  const dialog = machine && (
    <ResumeDialog
      open={open}
      onClose={() => setOpen(false)}
      sessions={sessions?.map((s) => sessionOf(s, machineName)) ?? null}
      failed={failure === null ? null : { reason: failure, onRetry: () => ask(machine) }}
      now={now}
      onResume={async (id) => {
        const session = sessions?.find((s) => s.sessionId === id);
        if (!session) return;
        const handle = resumeSession(machine, {
          provider: session.provider,
          sessionId: session.sessionId,
          cwd: session.cwd,
        });
        await settled(handle);
        const conv = await handle.conversation;
        if (conv) onOpened(conv);
      }}
    />
  );
  return { show, dialog };
};
