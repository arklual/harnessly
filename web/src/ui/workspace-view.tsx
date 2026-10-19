import { Spinner } from '@kit';
import { type PointerEvent as ReactPointerEvent, useEffect, useRef, useState } from 'react';

import type { LayoutNode, Workspace } from '../api/types';
import { clampRatio, panesOf } from '../lib/layout-tree';
import { layoutsStore, unwatchLayout, watchLayout } from '../stores/runtime';
import { useStore } from '../stores/store';
import { actions } from './actions';
import { PaneView } from './pane';

function Split({
  node,
  path,
  workspaceId,
  activePaneId,
  paneCount,
}: {
  node: Extract<LayoutNode, { type: 'split' }>;
  path: number[];
  workspaceId: string;
  activePaneId: string;
  paneCount: number;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [dragRatio, setDragRatio] = useState<number | null>(null);
  const row = node.orientation === 'horizontal';
  const ratio = dragRatio ?? node.ratio;

  const onPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const el = box.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    let last = node.ratio;
    const move = (ev: PointerEvent) => {
      const v = row
        ? ((ev.clientX - rect.left) / rect.width) * 100
        : ((ev.clientY - rect.top) / rect.height) * 100;
      last = clampRatio(v);
      setDragRatio(last);
    };
    const up = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', up);
      document.body.classList.remove('is-resizing');
      void actions.resize(workspaceId, path, last).then(() => setDragRatio(null));
    };
    document.body.classList.add('is-resizing');
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', up);
  };

  return (
    <div ref={box} className="split" data-orientation={row ? 'row' : 'column'}>
      <div className="split__child" style={{ flexBasis: `${ratio}%` }}>
        <NodeView
          node={node.children[0]}
          path={[...path, 0]}
          workspaceId={workspaceId}
          activePaneId={activePaneId}
          paneCount={paneCount}
        />
      </div>
      <div
        className="split__divider"
        role="separator"
        aria-orientation={row ? 'vertical' : 'horizontal'}
        aria-valuenow={Math.round(ratio)}
        aria-valuemin={10}
        aria-valuemax={90}
        tabIndex={0}
        onPointerDown={onPointerDown}
        onDoubleClick={() => void actions.equalize(workspaceId)}
        onKeyDown={(e) => {
          const step =
            e.key === (row ? 'ArrowLeft' : 'ArrowUp')
              ? -5
              : e.key === (row ? 'ArrowRight' : 'ArrowDown')
                ? 5
                : 0;
          if (step) {
            e.preventDefault();
            void actions.resize(workspaceId, path, clampRatio(node.ratio + step));
          }
        }}
      />
      <div className="split__child" style={{ flexBasis: `${100 - ratio}%` }}>
        <NodeView
          node={node.children[1]}
          path={[...path, 1]}
          workspaceId={workspaceId}
          activePaneId={activePaneId}
          paneCount={paneCount}
        />
      </div>
    </div>
  );
}

function NodeView(props: {
  node: LayoutNode;
  path: number[];
  workspaceId: string;
  activePaneId: string;
  paneCount: number;
}) {
  const { node, workspaceId, activePaneId, paneCount } = props;
  if (node.type === 'split') return <Split {...props} node={node} />;
  return (
    <PaneView
      pane={node}
      workspaceId={workspaceId}
      active={node.id === activePaneId}
      canClose={paneCount > 1}
    />
  );
}

export function WorkspaceView({ workspace }: { workspace: Workspace }) {
  const layout = useStore(layoutsStore, (all) => all[workspace.id]);

  useEffect(() => {
    watchLayout(workspace.id);
    return () => unwatchLayout(workspace.id);
  }, [workspace.id]);

  if (!layout) {
    return (
      <div className="shell__loading">
        <Spinner label="Loading the workspace" />
      </div>
    );
  }
  return (
    <div className="workspace">
      <NodeView
        node={layout.root}
        path={[]}
        workspaceId={workspace.id}
        activePaneId={layout.activePaneId}
        paneCount={panesOf(layout.root).length}
      />
    </div>
  );
}
