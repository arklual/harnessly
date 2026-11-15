import { useRef, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import usePaneSwipe from '@/next/features/shell/use-pane-swipe';
import usePaneWidth from '@/next/features/shell/use-pane-width';
import usePaneMotion, { paneExitMs, usePaneMax } from '@/next/features/shell/use-pane-motion';

export interface IPaneDrawerProps {
  open: boolean;
  onClose: () => void;
  /** Wide: the pane takes the whole stage on desktop. */
  wide?: boolean;
  /** The width kept for what the pane shows (the chat's); null: the device's. */
  width?: number | null;
  /** The person dragged the pane to this width. */
  onWidth?: (width: number) => void;
  /** On the phone the sheet takes the whole screen at once (a design); else it rises to two thirds. */
  full?: boolean;
  children: ReactNode;
}

/**
 * The right pane's frame: a column at the right of the stage on desktop; on the phone a bottom sheet over the chat
 * that rises to two thirds of the screen (a design: to all of it), opens to the whole screen with a drag up
 * and folds, then closes, with a drag down or a tap on the scrim. Its content is the pane feature's.
 */
const PaneDrawer = ({
  open,
  onClose,
  wide = false,
  width = null,
  onWidth,
  full = false,
  children,
}: IPaneDrawerProps) => {
  const paneRef = useRef<HTMLElement>(null);
  const scrimRef = useRef<HTMLDivElement>(null);
  const presence = usePresence(open ? true : null, paneExitMs());
  // The width first: the motion below measures the pane at it.
  const onSplit = usePaneWidth(paneRef, presence.value !== null, width, onWidth);
  usePaneMotion(paneRef, open);
  usePaneMax(paneRef, wide, presence.value !== null);
  usePaneSwipe(paneRef, scrimRef, open, onClose, full);
  if (presence.value === null) return null;
  return (
    <>
      <div
        ref={scrimRef}
        className="hx-pane-scrim"
        data-state={presence.state}
        aria-hidden="true"
        onClick={onClose}
      />
      <aside
        ref={paneRef}
        className="hx-pane"
        data-state={presence.state}
        data-max={wide || undefined}
        aria-label={t('next.pane.label')}
      >
        <div className="hx-pane__split" aria-hidden="true" onPointerDown={onSplit}>
          <span />
        </div>
        {/* The phone's handle: the grip to drag the sheet by, and a plain way back to the chat (a design's own «←» is
            its history, not the way out). */}
        <div className="hx-pane__grip">
          <span className="hx-pane__grip-bar" aria-hidden="true" />
          <button
            type="button"
            className="hx-head-btn hx-pane__grip-x"
            aria-label={t('next.pane.toChat')}
            onClick={onClose}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
        {children}
      </aside>
    </>
  );
};

export default PaneDrawer;
