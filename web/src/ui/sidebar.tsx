import { Button, Dialog, Dot, IconButton, Menu, TextField } from '@kit';
import { FolderPlus, MoreHorizontal, Pencil, Plus, Trash2 } from 'lucide-react';
import { type FormEvent, useMemo, useState } from 'react';

import { workspaces as workspacesApi } from '../api/endpoints';
import { activityOf, activityTone } from '../api/status';
import type { Workspace, WorkspaceList } from '../api/types';
import { refreshWorkspaces, reportError, statusStore } from '../stores/runtime';
import { useStore } from '../stores/store';
import { Logo } from './logo';

function useWorkspaceActivity() {
  const tabs = useStore(statusStore, (s) => s.tabs);
  return useMemo(() => {
    const by: Record<string, { working: number; attention: number }> = {};
    for (const entry of Object.values(tabs)) {
      if (entry.panelType !== 'claude-code' && entry.panelType !== 'codex-cli') continue;
      const a = activityOf(entry);
      const w = (by[entry.workspaceId] ??= { working: 0, attention: 0 });
      if (a === 'working' || a === 'starting') w.working++;
      if (a === 'waiting' || a === 'done') w.attention++;
    }
    return by;
  }, [tabs]);
}

function NewWorkspaceDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [name, setName] = useState('');
  const [directory, setDirectory] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const checkDirectory = async () => {
    if (!directory.trim()) return setError(null);
    try {
      const v = await workspacesApi.validate(directory.trim());
      if (!v.valid) setError(v.error ?? 'Not a folder');
      else {
        setError(null);
        const suggested = v.suggestedName;
        if (suggested) setName((cur) => (cur.trim() ? cur : suggested));
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const ws = await workspacesApi.create(name.trim(), directory.trim() || undefined);
      await refreshWorkspaces();
      onCreated(ws.id);
      setName('');
      setDirectory('');
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="New workspace"
      description="A workspace groups the terminals and agents of one project folder."
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button appearance="primary" type="submit" form="new-workspace" loading={busy}>
            Create
          </Button>
        </>
      }
    >
      <form id="new-workspace" onSubmit={submit} className="form-stack">
        <TextField
          label="Folder"
          placeholder="/path/to/project (your home folder when empty)"
          value={directory}
          onChange={(e) => setDirectory(e.target.value)}
          onBlur={checkDirectory}
          error={error}
          autoFocus
        />
        <TextField
          label="Name"
          placeholder="Taken from the folder"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
      </form>
    </Dialog>
  );
}

function RenameDialog({ workspace, onClose }: { workspace: Workspace | null; onClose: () => void }) {
  const [name, setName] = useState(workspace?.name ?? '');
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!workspace) return;
    setBusy(true);
    try {
      await workspacesApi.rename(workspace.id, name);
      await refreshWorkspaces();
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={workspace !== null}
      onClose={onClose}
      title="Rename workspace"
      size="s"
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button
            appearance="primary"
            type="submit"
            form="rename-workspace"
            loading={busy}
            disabled={!name.trim()}
          >
            Save
          </Button>
        </>
      }
    >
      <form id="rename-workspace" onSubmit={submit}>
        <TextField label="Name" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
      </form>
    </Dialog>
  );
}

function DeleteDialog({ workspace, onClose }: { workspace: Workspace | null; onClose: () => void }) {
  const [busy, setBusy] = useState(false);
  const remove = async () => {
    if (!workspace) return;
    setBusy(true);
    try {
      await workspacesApi.remove(workspace.id);
      await refreshWorkspaces();
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={workspace !== null}
      onClose={onClose}
      size="s"
      title={`Delete “${workspace?.name ?? ''}”?`}
      description="Its terminals and agents are stopped. The project folder stays as it is."
      footer={
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button appearance="danger" onClick={remove} loading={busy}>
            Delete
          </Button>
        </>
      }
    />
  );
}

export function Sidebar({
  list,
  currentId,
  onSelect,
}: {
  list: WorkspaceList | null;
  currentId: string | null;
  onSelect: (id: string) => void;
}) {
  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState<Workspace | null>(null);
  const [deleting, setDeleting] = useState<Workspace | null>(null);
  const activity = useWorkspaceActivity();

  return (
    <nav className="sidebar" aria-label="Workspaces">
      <div className="sidebar__brand">
        <Logo size={22} />
        <span>harnessly</span>
      </div>
      <div className="sidebar__section">
        <span className="sidebar__heading">Workspaces</span>
        <IconButton label="New workspace" size="xs" onClick={() => setCreating(true)}>
          <Plus size={14} />
        </IconButton>
      </div>
      <ul className="sidebar__list">
        {(list?.workspaces ?? []).map((w) => {
          const a = activity[w.id];
          return (
            <li key={w.id}>
              <div className="ws-row" data-active={w.id === currentId || undefined}>
                <button
                  type="button"
                  className="ws-row__main"
                  onClick={() => onSelect(w.id)}
                  aria-current={w.id === currentId ? 'page' : undefined}
                >
                  <span className="ws-row__name">{w.name}</span>
                  {a && a.working > 0 && (
                    <Dot tone={activityTone.working} live label={`${a.working} working`} />
                  )}
                  {a && a.working === 0 && a.attention > 0 && (
                    <Dot tone={activityTone.done} label={`${a.attention} finished`} />
                  )}
                </button>
                <Menu
                  label={`${w.name} actions`}
                  placement={{ side: 'bottom', align: 'end' }}
                  items={[
                    {
                      id: 'rename',
                      label: 'Rename',
                      icon: <Pencil size={14} />,
                      onSelect: () => setRenaming(w),
                    },
                    {
                      id: 'delete',
                      label: 'Delete',
                      icon: <Trash2 size={14} />,
                      danger: true,
                      onSelect: () => setDeleting(w),
                    },
                  ]}
                  trigger={(p) => (
                    <IconButton {...p} label={`${w.name} actions`} size="xs" className="ws-row__menu">
                      <MoreHorizontal size={14} />
                    </IconButton>
                  )}
                />
              </div>
            </li>
          );
        })}
      </ul>
      {list && list.workspaces.length === 0 && (
        <div className="sidebar__empty">
          <Button icon={<FolderPlus size={16} />} onClick={() => setCreating(true)}>
            New workspace
          </Button>
        </div>
      )}
      <NewWorkspaceDialog open={creating} onClose={() => setCreating(false)} onCreated={onSelect} />
      {renaming && <RenameDialog key={renaming.id} workspace={renaming} onClose={() => setRenaming(null)} />}
      <DeleteDialog workspace={deleting} onClose={() => setDeleting(null)} />
    </nav>
  );
}
