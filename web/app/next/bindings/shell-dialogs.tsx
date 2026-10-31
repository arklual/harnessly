// The shell's dialogs over the machine: «Новый проект» (a name), «Настройки проекта» (its name and folder) and
// «Продолжить сессию» (what terminal agents kept there). The last asks the machine when it opens; a list that did not
// come says why.
import { useMemo, useState } from 'react';
import { failureText, settled, pageText } from '@/next/bindings/outcome';
import { createProject, deleteProject, resumeSession, updateProject } from '@/next/commands';
import { loadDirs, loadSessions } from '@/next/commands/pages';
import NewProjectDialog from '@/next/features/sidebar/new-project-dialog';
import ProjectSettingsDialog, { type IProjectFolder } from '@/next/features/sidebar/project-settings-dialog';
import { projectKey, useProjects } from '@/next/stores/projects';
import { conversationsStore } from '@/next/stores/conversations';
import { useAll, type KeyedStore } from '@/next/core/store';
import { newProfileProjectId, withProject } from '@/next/features/sidebar/profile-layout';
import { editProfileSidebar } from '@/next/stores/profile-sidebar';
import ResumeDialog, { type IResumeSession } from '@/next/features/shell/resume-dialog';
import type { AgentSession, ConvId, Conversation, MachineId } from '@/next/protocol/v2';
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

type TChatCount = { count: number; working: number };

const chatsOfProjects = (s: KeyedStore<ConvId, Conversation>) => {
  const out = new Map<string, TChatCount>();
  for (const c of s.values()) {
    if (c.projectId === null) continue;
    const key = projectKey(c.machineId, c.projectId);
    const n = out.get(key) ?? { count: 0, working: 0 };
    out.set(key, { count: n.count + 1, working: n.working + (c.state.k === 'working' ? 1 : 0) });
  }
  return out;
};

const sameCounts = (a: Map<string, TChatCount>, b: Map<string, TChatCount>) =>
  a.size === b.size &&
  [...a].every(([k, n]) => b.get(k)?.count === n.count && b.get(k)?.working === n.working);

/** «Новый проект»: on the machine, or the person's, whose folders come on a machine as its chats do. */
export const useProjectDialog = (machine: MachineId | null, person: boolean) => {
  const [open, setOpen] = useState(false);

  const show = () => {
    if (machine) setOpen(true);
  };

  const dialog = machine && (
    <NewProjectDialog
      open={open}
      onClose={() => setOpen(false)}
      onCreate={async (name) => {
        if (person) {
          await editProfileSidebar((doc) => withProject(doc, name, newProfileProjectId(), Date.now()));
          return;
        }
        await settled(createProject(machine, name));
      }}
    />
  );
  return { show, dialog };
};

/** «Настройки проекта» of a machine's project; the person's projects (a profile across machines) have no folder of
 *  their own, so there is no dialog for them. */
export const useProjectSettingsDialog = (machine: MachineId | null, person: boolean) => {
  const [openId, setOpenId] = useState<string | null>(null);
  const projects = useProjects(person ? null : machine);
  const project = projects.find((p) => p.id === openId) ?? null;
  // the same object while the project is the same: the dialog takes its fields from it when it changes
  const name = project?.name;
  const path = project?.path;
  const current = useMemo(
    () => (name === undefined || path === undefined ? null : { name, path }),
    [name, path],
  );

  const [folders, setFolders] = useState<IProjectFolder[] | null>(null);
  // the chats of every project as the sidebar has them, what «Удалить проект» asks about; counted for all projects, as
  // the store's selection is kept by its version alone and must not depend on which one is open
  const counts = useAll(conversationsStore, chatsOfProjects, sameCounts);
  const counted = machine && openId !== null ? counts.get(projectKey(machine, openId)) : undefined;
  const all = counted?.count ?? 0;
  const working = counted?.working ?? 0;
  const chats = useMemo(() => ({ count: all, working }), [all, working]);

  // the machine's folders are asked each time it opens: a folder made since is offered too
  const show =
    person || !machine
      ? undefined
      : (id: string) => {
          setOpenId(id);
          setFolders(null);
          void loadDirs(machine).then((res) => {
            if ('ok' in res) setFolders(res.value.map(({ path, git, branch }) => ({ path, git, branch })));
          });
        };

  const dialog = machine && !person && (
    <ProjectSettingsDialog
      project={current}
      folders={folders}
      chats={chats}
      onClose={() => setOpenId(null)}
      onSave={async (change) => {
        if (!project) return;
        const st = await updateProject(machine, project.id, change).done;
        if (st.k !== 'failed') return;
        if (change.path && st.failure.r === 'refused' && st.failure.code === 'not-found')
          throw new Error(t('next.projectSettings.pathMissing'));
        throw new Error(failureText(st.failure));
      }}
      onDelete={async (fate) => {
        if (!project) return;
        const st = await deleteProject(machine, project.id, fate).done;
        if (st.k === 'failed') throw new Error(failureText(st.failure));
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
