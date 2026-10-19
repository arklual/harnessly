import { IconButton, Menu, Spinner, Tooltip, useTheme } from '@kit';
import { LogOut, Monitor, Moon, PanelLeft, Sun } from 'lucide-react';
import { useEffect, useState } from 'react';

import { workspaces as workspacesApi } from '../api/endpoints';
import { useStore } from '../stores/store';
import { connectionStore, workspacesStore } from '../stores/runtime';
import { MachineButton } from './machine-panel';
import { Sidebar } from './sidebar';
import { Toasts } from './toasts';
import { WorkspaceView } from './workspace-view';

function hashWorkspace(): string | null {
  const m = /^#\/w\/([\w-]+)/.exec(window.location.hash);
  return m?.[1] ?? null;
}

function ThemeMenu() {
  const { mode, resolved, setMode } = useTheme();
  const icon = (m: string) =>
    m === 'light' ? <Sun size={16} /> : m === 'dark' ? <Moon size={16} /> : <Monitor size={16} />;
  return (
    <Menu
      label="Theme"
      placement={{ side: 'bottom', align: 'end' }}
      items={(['light', 'dark', 'system'] as const).map((m) => ({
        id: m,
        label: m === 'system' ? 'System' : m === 'light' ? 'Light' : 'Dark',
        icon: icon(m),
        hint: mode === m ? '✓' : undefined,
        onSelect: () => setMode(m),
      }))}
      trigger={(p) => (
        <IconButton {...p} label="Theme" size="s">
          {icon(resolved)}
        </IconButton>
      )}
    />
  );
}

export function Shell({ onSignOut }: { onSignOut: () => void }) {
  const list = useStore(workspacesStore);
  const conn = useStore(connectionStore);
  const [selected, setSelected] = useState<string | null>(hashWorkspace);
  const [collapsed, setCollapsed] = useState(false);

  const all = list?.workspaces ?? [];
  const current =
    all.find((w) => w.id === selected) ?? all.find((w) => w.id === list?.activeWorkspaceId) ?? all[0] ?? null;

  useEffect(() => {
    const onHash = () => setSelected(hashWorkspace());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  useEffect(() => {
    if (current && hashWorkspace() !== current.id) window.history.replaceState(null, '', `#/w/${current.id}`);
    if (current && list && list.activeWorkspaceId !== current.id)
      void workspacesApi.setActive(current.id).catch(() => undefined);
    // the active workspace is remembered by the runtime once per change
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current?.id]);

  const select = (id: string) => {
    setSelected(id);
    window.history.pushState(null, '', `#/w/${id}`);
  };

  const offline = conn.sync !== 'open' || conn.status !== 'open';

  return (
    <div className="shell" data-collapsed={collapsed || undefined}>
      {!collapsed && <Sidebar list={list} currentId={current?.id ?? null} onSelect={select} />}
      <main className="shell__main">
        <header className="topbar">
          <IconButton
            label={collapsed ? 'Show sidebar' : 'Hide sidebar'}
            size="s"
            onClick={() => setCollapsed((c) => !c)}
          >
            <PanelLeft size={16} />
          </IconButton>
          <div className="topbar__title">{current?.name ?? 'No workspace'}</div>
          {current && <div className="topbar__path">{current.directories[0]}</div>}
          <div className="topbar__spacer" />
          {offline && (
            <Tooltip content="Reconnecting to the runtime">
              <span className="topbar__offline">
                <Spinner size="xs" /> Reconnecting
              </span>
            </Tooltip>
          )}
          <MachineButton />
          <ThemeMenu />
          <IconButton label="Sign out" size="s" onClick={onSignOut}>
            <LogOut size={16} />
          </IconButton>
        </header>
        <div className="shell__content">
          {list === null ? (
            <div className="shell__loading">
              <Spinner label="Loading workspaces" />
            </div>
          ) : current ? (
            <WorkspaceView key={current.id} workspace={current} />
          ) : (
            <div className="shell__loading">Create a workspace to start.</div>
          )}
        </div>
      </main>
      <Toasts />
    </div>
  );
}
