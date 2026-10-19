import { Dot, EmptyState, IconButton, Menu } from '@kit';
import { Bot, Columns2, MoreHorizontal, Plus, Rows2, SquareTerminal, X, Equal, Sparkles } from 'lucide-react';
import { type DragEvent, useState } from 'react';

import { activityOf, activityTone } from '../api/status';
import type { PaneNode, Tab } from '../api/types';
import { isAgentTab, moveId, sortedTabs, tabLabel } from '../lib/layout-tree';
import { statusStore } from '../stores/runtime';
import { useStore } from '../stores/store';
import { actions } from './actions';
import { AgentView } from './agent-view';
import { TerminalView } from './terminal-view';

const DRAG_TYPE = 'application/x-layout-tab';

function TabIcon({ tab }: { tab: Tab }) {
  if (tab.panelType === 'claude-code') return <Sparkles size={14} />;
  if (tab.panelType === 'codex-cli') return <Bot size={14} />;
  return <SquareTerminal size={14} />;
}

function TabButton({
  tab,
  active,
  workspaceId,
  paneId,
  onDropBefore,
}: {
  tab: Tab;
  active: boolean;
  workspaceId: string;
  paneId: string;
  onDropBefore: (e: DragEvent) => void;
}) {
  const entry = useStore(statusStore, (s) => s.tabs[tab.id]);
  const agent = isAgentTab(tab);
  const activity = activityOf(entry);
  const [over, setOver] = useState(false);
  return (
    <div
      className="tab"
      role="tab"
      aria-selected={active}
      data-active={active || undefined}
      data-drop={over || undefined}
      data-testid={`tab-${tab.id}`}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, JSON.stringify({ paneId, tabId: tab.id }));
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes(DRAG_TYPE)) {
          e.preventDefault();
          setOver(true);
        }
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        setOver(false);
        onDropBefore(e);
      }}
      onClick={() => !active && void actions.selectTab(workspaceId, paneId, tab.id)}
      onAuxClick={(e) => e.button === 1 && void actions.closeTab(workspaceId, paneId, tab.id)}
      title={tab.cwd}
    >
      <span className="tab__icon">
        <TabIcon tab={tab} />
      </span>
      <span className="tab__label">{tabLabel(tab)}</span>
      {agent && activity !== 'idle' && activity !== 'offline' && (
        <Dot tone={activityTone[activity]} live={activity === 'working'} label={activity} />
      )}
      <button
        type="button"
        className="tab__close"
        aria-label={`Close ${tabLabel(tab)}`}
        onClick={(e) => {
          e.stopPropagation();
          void actions.closeTab(workspaceId, paneId, tab.id);
        }}
      >
        <X size={12} />
      </button>
    </div>
  );
}

export function PaneView({
  pane,
  workspaceId,
  active,
  canClose,
}: {
  pane: PaneNode;
  workspaceId: string;
  active: boolean;
  canClose: boolean;
}) {
  const tabs = sortedTabs(pane);
  const current = tabs.find((t) => t.id === pane.activeTabId) ?? tabs[0];
  const [endOver, setEndOver] = useState(false);

  const dropAt = (e: DragEvent, index: number) => {
    const raw = e.dataTransfer.getData(DRAG_TYPE);
    if (!raw) return;
    e.preventDefault();
    const { paneId: from, tabId } = JSON.parse(raw) as { paneId: string; tabId: string };
    if (from === pane.id) {
      const ids = tabs.map((t) => t.id);
      const fromIndex = ids.indexOf(tabId);
      const to = fromIndex < index ? index - 1 : index;
      const next = moveId(ids, tabId, to);
      if (next.join() !== ids.join()) void actions.reorder(workspaceId, pane.id, next);
    } else {
      void actions.move(workspaceId, from, tabId, pane.id, index);
    }
  };

  const newItems = [
    {
      id: 'terminal',
      label: 'Terminal',
      icon: <SquareTerminal size={14} />,
      onSelect: () => void actions.newTerminal(workspaceId, pane.id),
    },
    {
      id: 'claude',
      label: 'Claude Code',
      icon: <Sparkles size={14} />,
      onSelect: () => void actions.newAgent(workspaceId, pane.id, 'claude'),
    },
    {
      id: 'codex',
      label: 'Codex',
      icon: <Bot size={14} />,
      onSelect: () => void actions.newAgent(workspaceId, pane.id, 'codex'),
    },
  ];

  return (
    <section
      className="pane"
      data-active={active || undefined}
      data-testid={`pane-${pane.id}`}
      aria-label="Pane"
      onPointerDownCapture={() => !active && void actions.focusPane(workspaceId, pane.id)}
    >
      <div className="pane__bar">
        <div className="pane__tabs" role="tablist">
          {tabs.map((t, i) => (
            <TabButton
              key={t.id}
              tab={t}
              active={t.id === current?.id}
              workspaceId={workspaceId}
              paneId={pane.id}
              onDropBefore={(e) => dropAt(e, i)}
            />
          ))}
          <div
            className="pane__tabs-end"
            data-drop={endOver || undefined}
            onDragOver={(e) => {
              if (e.dataTransfer.types.includes(DRAG_TYPE)) {
                e.preventDefault();
                setEndOver(true);
              }
            }}
            onDragLeave={() => setEndOver(false)}
            onDrop={(e) => {
              setEndOver(false);
              dropAt(e, tabs.length);
            }}
          >
            <Menu
              label="New tab"
              items={newItems}
              trigger={(p) => (
                <IconButton {...p} label="New tab" size="xs">
                  <Plus size={14} />
                </IconButton>
              )}
            />
          </div>
        </div>
        <Menu
          label="Pane actions"
          placement={{ side: 'bottom', align: 'end' }}
          items={[
            {
              id: 'right',
              label: 'Split right',
              icon: <Columns2 size={14} />,
              onSelect: () => void actions.split(workspaceId, pane.id, 'horizontal', current?.cwd),
            },
            {
              id: 'down',
              label: 'Split down',
              icon: <Rows2 size={14} />,
              onSelect: () => void actions.split(workspaceId, pane.id, 'vertical', current?.cwd),
            },
            {
              id: 'equal',
              label: 'Equal sizes',
              icon: <Equal size={14} />,
              onSelect: () => void actions.equalize(workspaceId),
            },
            'separator',
            {
              id: 'close',
              label: 'Close pane',
              icon: <X size={14} />,
              danger: true,
              disabled: !canClose,
              onSelect: () => void actions.closePane(workspaceId, pane.id),
            },
          ]}
          trigger={(p) => (
            <IconButton {...p} label="Pane actions" size="xs">
              <MoreHorizontal size={14} />
            </IconButton>
          )}
        />
      </div>
      <div className="pane__body">
        {current ? (
          isAgentTab(current) ? (
            <AgentView key={current.id} tab={current} workspaceId={workspaceId} paneId={pane.id} />
          ) : (
            <TerminalView
              key={current.id}
              session={current.sessionName}
              focus={active}
              onRestart={() => actions.restart(workspaceId, pane.id, current.id)}
            />
          )
        ) : (
          <EmptyState
            icon={<SquareTerminal size={20} />}
            title="No tabs"
            actions={
              <Menu
                label="New tab"
                items={newItems}
                trigger={(p) => (
                  <IconButton {...p} label="New tab" appearance="outline" size="s">
                    <Plus size={16} />
                  </IconButton>
                )}
              />
            }
          >
            Open a terminal or start an agent in this pane.
          </EmptyState>
        )}
      </div>
    </section>
  );
}
