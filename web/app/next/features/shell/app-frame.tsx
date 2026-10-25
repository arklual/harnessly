import { useEffect, useRef, useState, type ReactNode } from 'react';
import { PortalContext } from '@/next/ui/portal';
import useDrawerSwipe from '@/next/features/shell/use-drawer-swipe';
import useSideMotion from '@/next/features/shell/use-side-motion';
import useRailPeek from '@/next/features/shell/use-rail-peek';
import { PRESENCE } from '@/next/ui/presence';
import { useCssTips } from '@/next/ui/css-tips';
import { usePhone } from '@/next/ui/use-media';
import { drawerState } from '@/next/features/shell/drawer-state';

export interface IAppFrameProps {
  /** The desktop sidebar is folded to its rail. */
  rail: boolean;
  /** The phone drawer is out. */
  drawerOpen: boolean;
  onDrawerChange: (open: boolean) => void;
  sidebar: ReactNode;
  /** The strip of tabs over the window (null: none, the window takes the whole height). */
  tabs?: ReactNode;
  /** The main window: header, stage. */
  children: ReactNode;
  /** The right pane: it stands on the window's body beside the stage (null: closed). */
  aside?: ReactNode;
  /** Layers over everything: dialogs, the search, toasts. */
  overlays?: ReactNode;
  /** The person's text size (Settings): the chat's messages and field are scaled by it, see chat.css. */
  textSize?: 's' | 'm' | 'l';
}

const PLAY = { wait: '0', on: '1', done: 'done' } as const;
type TPlay = (typeof PLAY)[keyof typeof PLAY];
/** The longest cascade of the entrance: the last day-group row starts at 560 + 24 × 45 ms and runs 560 ms. */
const ENTRANCE_MS = 2400;

/**
 * The page of the new interface: one dark surface, as the design has it. The sidebar lies on the page itself, the
 * window beside it is set apart by its tone only (no frames), and the right pane stands in the window. On the phone
 * the sidebar is a drawer over a full-screen window, pulled by a finger and dimmed behind, and the pane is a sheet.
 * Popups mount into its own portal, inside `.hx-root`.
 */
const AppFrame = ({
  rail: wantRail,
  drawerOpen,
  onDrawerChange,
  sidebar,
  tabs = null,
  children,
  aside = null,
  overlays,
  textSize = 'm',
}: IAppFrameProps) => {
  const phone = usePhone();
  const { drawer, inert, rail } = drawerState(drawerOpen, phone, wantRail);
  const appRef = useRef<HTMLDivElement>(null);
  const scrimRef = useRef<HTMLDivElement>(null);
  const portalRef = useRef<HTMLDivElement>(null);
  const mainRef = useRef<HTMLElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const stripRef = useRef<HTMLDivElement>(null);
  useDrawerSwipe({ app: appRef, scrim: scrimRef }, drawer === 'open', onDrawerChange);
  useSideMotion({ app: appRef, main: mainRef, body: bodyRef, strip: stripRef }, rail);
  // The folded sidebar opens over the window under a resting pointer: it is drawn open (data-side) while the grid
  // keeps the rail's column (data-peek); folding back, it is the rail again, narrowing over the window.
  const peek = useRailPeek(appRef, rail);
  // The CSS hints of the whole interface: one delay, no wait along a row, never over their control.
  useCssTips(appRef);
  const side = rail && peek !== PRESENCE.open ? 'closed' : 'open';
  // The entrance plays once the first frame is painted: sidebar from the left, the window, then the cascade inside.
  // Once it has played, what mounts later (another chat, a new row) has its own motion and does not replay it.
  const [play, setPlay] = useState<TPlay>(PLAY.wait);
  const [hidden, setHidden] = useState(() => document.hidden);
  useEffect(() => {
    const id = requestAnimationFrame(() => setPlay(PLAY.on));
    const onVis = () => setHidden(document.hidden);
    document.addEventListener('visibilitychange', onVis);
    return () => {
      cancelAnimationFrame(id);
      document.removeEventListener('visibilitychange', onVis);
    };
  }, []);
  useEffect(() => {
    if (play !== PLAY.on || hidden) return;
    const timer = setTimeout(() => setPlay(PLAY.done), ENTRANCE_MS);
    return () => clearTimeout(timer);
  }, [play, hidden]);

  return (
    <PortalContext value={portalRef}>
      <div
        ref={appRef}
        className="hx-root hx-app"
        data-drawer={drawer}
        data-side={side}
        data-peek={peek ?? undefined}
        data-tabs={tabs ? 'on' : undefined}
        data-text={textSize}
        data-play={play}
        data-hidden={hidden || undefined}
      >
        <div ref={scrimRef} className="hx-scrim" aria-hidden="true" onClick={() => onDrawerChange(false)} />
        {sidebar}
        {tabs && (
          <div ref={stripRef} className="hx-app__tabs">
            {tabs}
          </div>
        )}
        <main
          ref={mainRef}
          className="hx-main hx-enter"
          data-eb="120"
          data-ed="700"
          inert={inert || undefined}
        >
          <div ref={bodyRef} className="hx-main__body">
            {children}
            {aside}
          </div>
        </main>
        {overlays}
        <div ref={portalRef} className="hx-portal" />
      </div>
    </PortalContext>
  );
};

export default AppFrame;
