import { IconButton, Menu, Spinner, Tooltip, useTheme } from '@kit';
import { LogOut, Monitor, Moon, PanelLeft, PanelRight, Sun } from 'lucide-react';
import { type PointerEvent as ReactPointerEvent, useEffect, useState } from 'react';

import { workspaces as workspacesApi } from '../api/endpoints';
import { useStore } from '../stores/store';
import { connectionStore, workspacesStore } from '../stores/runtime';
import { integrationsStore, loadIntegrations, showPane, sidePaneStore } from '../stores/side-pane';
import { MachineButton } from './machine-panel';
import { SidePane } from './side-pane';
import { Sidebar } from './sidebar';
import { Toasts } from './toasts';
import { WorkspaceView } from './workspace-view';

function hashWorkspace(): string | null {
  const m = /^#\/w\/([\w-]+)/.exec(window.location.hash);
  return m?.[1] ?? null;
}

const WIDTH_KEY = 'side-pane-width';

function savedWidth(): number {
  try {
    const v = Number(localStorage.getItem(WIDTH_KEY));
    return Number.isFinite(v) && v >= 320 ? v : 520;
  } catch {
    return 520;
  }
}

/** The divider between the workspace and the side pane: drag to resize, double-click for the default width. */
function SideDivider({ width, onWidth }: { width: number; onWidth: (w: number) => void }) {
  const clamp = (w: number) => Math.round(Math.max(320, Math.min(window.innerWidth * 0.7, w)));
  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startX = e.clientX;
    const start = width;
    let last = start;
    const move = (ev: PointerEvent) => {
      last = clamp(start + startX - ev.clientX);
      onWidth(last);
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      document.body.classList.remove('is-resizing');
      try {
        localStorage.setItem(WIDTH_KEY, String(last));
      } catch {
        // the width is a convenience
      }
    };
    document.body.classList.add('is-resizing');
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };
  return (
    <div
      className="split__divider side-divider"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the side pane"
      aria-valuenow={width}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onDoubleClick={() => onWidth(520)}
      onKeyDown={(e) => {
        const step = e.key === 'ArrowLeft' ? 40 : e.key === 'ArrowRight' ? -40 : 0;
        if (step) {
          e.preventDefault();
          onWidth(clamp(width + step));
        }
      }}
    />
  );
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
  const services = useStore(integrationsStore);
  const sideOpen = useStore(sidePaneStore, (s) => s.open);
  const [sideWidth, setSideWidth] = useState(savedWidth);
  const hasServices = Boolean(services.boards || services.artifacts);

  useEffect(() => {
    void loadIntegrations();
  }, []);

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
          {hasServices && (
            <IconButton
              label={sideOpen ? 'Hide boards and documents' : 'Boards and documents'}
              size="s"
              aria-pressed={sideOpen}
              data-testid="side-pane-toggle"
              onClick={() =>
                showPane(!sideOpen, sideOpen ? undefined : services.boards ? undefined : 'document')
              }
            >
              <PanelRight size={16} />
            </IconButton>
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
            <div className="shell__work">
              <WorkspaceView key={current.id} workspace={current} />
              {hasServices && sideOpen && (
                <>
                  <SideDivider width={sideWidth} onWidth={setSideWidth} />
                  <div className="shell__side" style={{ width: sideWidth }}>
                    <SidePane workspaceId={current.id} />
                  </div>
                </>
              )}
            </div>
          ) : (
            <div className="shell__loading">Create a workspace to start.</div>
          )}
        </div>
      </main>
      <Toasts />
    </div>
  );
}
