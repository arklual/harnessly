import { t } from '@/lib/i18n';
import { useEffect, useState, type CSSProperties, type MouseEvent, type PointerEvent } from 'react';
import { ExternalLink, PanelRight, X } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton, cx } from '@/components/agents/ui';
import {
  FLOATING_BREAKPOINT,
  PreviewBody,
  forgetAutoOpen,
  openedNeighbours,
  reopenTarget,
  shownGateKey,
  targetGateKey,
  usePreviewOpener,
} from '@/components/agents/inspector/preview-island';
import { useOverloadGate } from '@/components/agents/machine-load';
import { DevtoolsDrawer, DevtoolsToggle } from '@/components/agents/devtools/devtools-drawer';
import { useDevtoolsDrawer, usePreviewDevtools } from '@/hooks/use-preview-devtools';
import useDesignLink from '@/hooks/use-design-link';
import { useLibraryPort } from '@/hooks/use-design-library';
import useIsMobile from '@/hooks/use-is-mobile';
import { currentMachineId } from '@/lib/app-path';
import { parseLibraryBoardPath } from '@/lib/hl-design-library/phone';
import {
  FLOATING_EDGES,
  FLOATING_MIN,
  applyDrag,
  applyResize,
  clampToViewport,
  gestureLayout,
  fitFloatingLayout,
  loadFloatingLayout,
  saveFloatingLayout,
  toggleMaximised,
  type IFloatingLayout,
  type TEdge,
} from '@/lib/hl-floating-geometry';
import { targetLabel, type TPreviewTarget } from '@/lib/hl-preview-client';
import { relatedPorts } from '@/lib/hl-preview/ports-shared';
import { usePreviewState } from '@/lib/hl-preview-store';
import type { ITab } from '@/types/terminal';

const viewportSize = () => ({ width: window.innerWidth, height: window.innerHeight });

const layoutStorage = (): Storage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

interface IGesture {
  kind: 'drag' | TEdge;
  pointerId: number;
  startX: number;
  startY: number;
  start: IFloatingLayout;
}

const fromButton = (event: { target: EventTarget }) =>
  event.target instanceof Element && event.target.closest('button') !== null;

const FloatingWindow = ({ previewKey, onDock }: { previewKey: string; onDock: () => void }) => {
  const [{ state, ports }, patch] = usePreviewState(previewKey);
  const { open, openInTab, close, openingTab, tabError } = usePreviewOpener(previewKey);
  const { guard, gate } = useOverloadGate(previewKey);
  const { openFull } = useDesignLink();
  const libraryPort = useLibraryPort();
  const [machine] = useState(() => currentMachineId() ?? '');
  const [layout, setLayout] = useState<IFloatingLayout>(() =>
    loadFloatingLayout(layoutStorage(), machine, viewportSize()),
  );
  const [gesture, setGesture] = useState<IGesture | null>(null);
  const [drawer, patchDrawer] = useDevtoolsDrawer(previewKey);
  const openPage = state.kind === 'open' ? state : null;
  const devtools = usePreviewDevtools({
    slot: openPage?.preview.origin ?? null,
    machineId: openPage?.preview.machineId,
    open: drawer.open,
  });

  useEffect(() => {
    const fit = () => setLayout((current) => fitFloatingLayout(current, viewportSize()));
    window.addEventListener('resize', fit);
    return () => window.removeEventListener('resize', fit);
  }, []);

  const commit = (next: IFloatingLayout) => {
    setLayout(next);
    saveFloatingLayout(layoutStorage(), machine, next);
  };

  // Pointer capture keeps the gesture alive over the iframe and outside the window; the iframe also stops taking pointers.
  const begin = (kind: IGesture['kind']) => (event: PointerEvent<HTMLElement>) => {
    if (event.button !== 0 || (kind === 'drag' && fromButton(event))) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    setGesture({
      kind,
      pointerId: event.pointerId,
      startX: event.clientX,
      startY: event.clientY,
      start: layout,
    });
  };
  const move = (event: PointerEvent<HTMLElement>) => {
    if (gesture?.pointerId !== event.pointerId) return;
    const dx = event.clientX - gesture.startX;
    const dy = event.clientY - gesture.startY;
    const { rect: from } = gesture.start;
    const moved =
      gesture.kind === 'drag'
        ? applyDrag(from, dx, dy)
        : applyResize(from, gesture.kind, dx, dy, FLOATING_MIN, viewportSize());
    setLayout(gestureLayout(gesture.start, clampToViewport(moved, viewportSize(), FLOATING_MIN)));
  };
  // Up, cancel and lost capture all keep the last applied layout: a cancelled pointer has no trustworthy position.
  const end = (event: PointerEvent<HTMLElement>) => {
    if (gesture?.pointerId !== event.pointerId) return;
    setGesture(null);
    saveFloatingLayout(layoutStorage(), machine, layout);
  };
  const gestureHandlers = {
    onPointerMove: move,
    onPointerUp: end,
    onPointerCancel: end,
    onLostPointerCapture: end,
  };

  const toggleSize = (event: MouseEvent<HTMLElement>) => {
    if (!fromButton(event)) commit(toggleMaximised(layout, viewportSize()));
  };

  const { rect } = layout;
  const geometry = {
    '--tw-float-x': `${rect.x}px`,
    '--tw-float-y': `${rect.y}px`,
    '--tw-float-w': `${rect.width}px`,
    '--tw-float-h': `${rect.height}px`,
  } as CSSProperties;
  const address =
    state.kind === 'open' || state.kind === 'taken'
      ? targetLabel(state.target)
      : t('inspector.floatingPreview.preview');
  const reload = (target: TPreviewTarget, neighbours: readonly number[]) => {
    forgetAutoOpen(previewKey);
    void open(target, neighbours);
  };
  // A board of the design library opens in a window of its own by its link, not through a one-time slot ticket.
  const board =
    state.kind === 'open' && state.target.kind === 'port' && state.target.port === libraryPort
      ? parseLibraryBoardPath(state.path)
      : null;
  const openInWindow = () => {
    if (board) openFull({ project: board.artifact, board: board.board, full: true });
    else if (state.kind === 'open') void openInTab(reopenTarget(state), openedNeighbours(state));
  };

  return (
    <section
      className={cx('tw-floating-preview', gesture && 'tw-floating-preview-moving')}
      style={geometry}
      role="dialog"
      aria-modal="false"
      aria-label={t('inspector.floatingPreview.windowLabel', { address })}
    >
      <header
        className="tw-floating-preview-header"
        title={t('inspector.floatingPreview.dragHint')}
        onPointerDown={begin('drag')}
        onDoubleClick={toggleSize}
        {...gestureHandlers}
      >
        <span className="tw-preview-address">{address}</span>
        {state.kind === 'open' && (
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            aria-label={t('inspector.floatingPreview.openInNewTab')}
            title={t('inspector.floatingPreview.openInNewTab')}
            disabled={openingTab}
            onClick={openInWindow}
          >
            <ExternalLink />
          </TwButton>
        )}
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          aria-label={t('inspector.floatingPreview.dock')}
          title={t('inspector.floatingPreview.dock')}
          onClick={() => {
            patch({ detached: false });
            onDock();
          }}
        >
          <PanelRight />
        </TwButton>
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.ghost}
          aria-label={t('inspector.floatingPreview.close')}
          title={t('inspector.floatingPreview.close')}
          onClick={close}
        >
          <X />
        </TwButton>
      </header>
      <div className="tw-floating-preview-body">
        {tabError && (
          <p className="tw-preview-error" role="alert">
            {tabError}
          </p>
        )}
        {gate}
        <PreviewBody
          previewKey={previewKey}
          state={state}
          onReopenTaken={(taken) =>
            reload(
              reopenTarget(taken),
              taken.target.kind === 'port' ? relatedPorts(ports ?? [], taken.target.port) : [],
            )
          }
          onStale={(current) => void open(reopenTarget(current), openedNeighbours(current))}
          onEntered={(current) => patch({ state: { ...current, entered: true } })}
          onReload={(current) => reload(reopenTarget(current), openedNeighbours(current))}
          onOpenUrl={(target) => guard(targetGateKey(target), shownGateKey(state), () => reload(target, []))}
          actions={
            openPage && (
              <DevtoolsToggle
                devtools={devtools}
                open={drawer.open}
                onToggle={() => patchDrawer({ open: !drawer.open })}
              />
            )
          }
        />
        {openPage && drawer.open && (
          <DevtoolsDrawer
            devtools={devtools}
            slot={openPage.preview.origin}
            machineId={openPage.preview.machineId}
            tab={drawer.tab}
            onTab={(tab) => patchDrawer({ tab })}
            onClose={() => patchDrawer({ open: false })}
          />
        )}
      </div>
      {FLOATING_EDGES.map((edge) => (
        <div
          key={edge}
          className={`tw-floating-preview-edge tw-floating-preview-edge-${edge}`}
          aria-hidden
          onPointerDown={begin(edge)}
          {...gestureHandlers}
        />
      ))}
    </section>
  );
};

/**
 * The preview of the active tab floating above the chat, moved by its header and resized by its edges.
 * Below the inspector breakpoint there is no room for it and the preview stays in the right panel.
 */
const FloatingPreview = ({ tab, onDock }: { tab: ITab | null; onDock: () => void }) => {
  const key = tab?.id ?? '';
  const [{ state, detached }] = usePreviewState(key);
  const narrow = useIsMobile(FLOATING_BREAKPOINT);
  if (!tab || !detached || narrow || state.kind === 'idle') return null;
  return <FloatingWindow key={key} previewKey={key} onDock={onDock} />;
};

export default FloatingPreview;
