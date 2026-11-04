// The video's left panel, «Чат | Комментарии», in the deck panel's classes: docked beside the stage, floating over it,
// or off; on a phone a bottom sheet with a handle (thresholds in deck/panel-sheet.ts). The chat tab hosts the site
// chat; the comments tab is the slot of VideoComments. The chat's field wears the context the view gives it.
import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react';
import { PANEL_DROP, panelDrop, panelFollow } from '@/next/features/designly/deck/panel-sheet';
import { DECK_PANEL, type TDeckPanelMode } from '@/next/features/designly/deck/types';
import type { ISiteStage } from '@/next/features/designly/site-view';
import { t } from '@/next/i18n';
import { ChatTerminalButton } from '@/next/features/terminal/chat-terminal';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';

const EXIT_MS = 170;
const SHEET_EXIT_MS = 240;

export const VIDEO_TAB = { chat: 'chat', comments: 'comments' } as const;
export type TVideoTab = (typeof VIDEO_TAB)[keyof typeof VIDEO_TAB];

const TABS: readonly { id: TVideoTab; key: string }[] = [
  { id: VIDEO_TAB.chat, key: 'next.video.shell.chat' },
  { id: VIDEO_TAB.comments, key: 'next.video.shell.comments' },
];

export interface IVideoChatContext {
  label: string;
  text: string;
  selected: boolean;
  /** `text` is a whole `<annotation kind="video">` block (a stretch sent to the chat). */
  block: boolean;
}

export interface IVideoPanelProps {
  mode: TDeckPanelMode;
  tab: TVideoTab;
  onMode: (mode: TDeckPanelMode) => void;
  onTab: (tab: TVideoTab) => void;
  phone: boolean;
  /** The chat's node (the site stage's `deckChat`). */
  chat: ReactNode;
  /** The comments tab's body. */
  comments: ReactNode;
  /** What the chat's field wears while the chat shows; null for nothing. */
  context: IVideoChatContext | null;
  site?: Pick<ISiteStage, 'context'>;
}

/** The handle of the phone sheet: a tap toggles the tall sheet, a pull makes it tall, comes back or closes it. */
const useSheetGrab = (panel: HTMLElement | null, onClose: () => void) => {
  const [tall, setTall] = useState(false);
  const [dragging, setDragging] = useState(false);
  const from = useRef<{ y: number; t: number } | null>(null);
  const reset = () => {
    from.current = null;
    setDragging(false);
    panel?.style.setProperty('--pd', '0px');
  };
  const onDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture?.(event.pointerId);
    from.current = { y: event.clientY, t: performance.now() };
    setDragging(true);
    panel?.style.setProperty('--pd', '0px');
  };
  const onMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (!from.current) return;
    const dy = event.clientY - from.current.y;
    panel?.style.setProperty('--pd', `${Math.round(panelFollow(dy, tall, window.innerHeight))}px`);
  };
  const onUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    const start = from.current;
    if (!start) return;
    const drop = panelDrop(event.clientY - start.y, performance.now() - start.t, tall);
    reset();
    if (drop === PANEL_DROP.tap) setTall(!tall);
    else if (drop === PANEL_DROP.tall) setTall(true);
    else if (drop === PANEL_DROP.normal) setTall(false);
    else if (drop === PANEL_DROP.close) {
      setTall(false);
      onClose();
    }
  };
  return {
    tall,
    dragging,
    grab: { onPointerDown: onDown, onPointerMove: onMove, onPointerUp: onUp, onPointerCancel: onUp },
  };
};

const VideoPanel = ({ mode, tab, onMode, onTab, phone, chat, comments, context, site }: IVideoPanelProps) => {
  const shown = usePresence(mode === DECK_PANEL.off ? null : mode, phone ? SHEET_EXIT_MS : EXIT_MS);
  const [panel, setPanel] = useState<HTMLElement | null>(null);
  const sheet = useSheetGrab(panel, () => onMode(DECK_PANEL.off));

  const chip = shown.value !== null && tab === VIDEO_TAB.chat ? context : null;
  const label = chip?.label ?? null;
  const text = chip?.text ?? null;
  const selected = chip?.selected ?? false;
  const block = chip?.block ?? false;
  const put = useRef(site?.context);
  useLayoutEffect(() => {
    put.current = site?.context;
  });
  useEffect(() => {
    put.current?.(label === null || text === null ? null : { label, text, selected, block });
    return () => put.current?.(null);
  }, [label, text, selected, block]);

  if (shown.value === null) return null;
  const closing = shown.state === PRESENCE.closing;
  const floating = shown.value === DECK_PANEL.float;
  return (
    <section
      ref={setPanel}
      className="hx-deck-panel hx-video-panel"
      data-closing={closing ? '1' : '0'}
      data-tall={sheet.tall ? '1' : '0'}
      data-drag={sheet.dragging ? '1' : '0'}
      aria-label={t('next.video.shell.panelLabel')}
    >
      <div className="hx-deck-pgrab" aria-label={t('next.video.shell.grab')} {...sheet.grab} />
      <div className="hx-deck-ph">
        <div className="hx-deck-seg" role="tablist" aria-label={t('next.video.shell.panelLabel')}>
          {TABS.map(({ id, key }) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={tab === id}
              data-on={tab === id ? '1' : '0'}
              onClick={() => onTab(id)}
            >
              {t(key)}
            </button>
          ))}
        </div>
        <ChatTerminalButton />
        <button
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          data-deck-float=""
          type="button"
          aria-label={floating ? t('next.video.shell.dock') : t('next.video.shell.float')}
          onClick={() => onMode(floating ? DECK_PANEL.dock : DECK_PANEL.float)}
        >
          <Icon name={floating ? 'panel-left' : 'maximize-2'} size="sm" />
        </button>
        <button
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          type="button"
          aria-label={t('next.video.shell.hide')}
          onClick={() => onMode(DECK_PANEL.off)}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {tab === VIDEO_TAB.chat && (
        <div className="hx-deck-chat" role="tabpanel">
          {chat}
        </div>
      )}
      {tab === VIDEO_TAB.comments && (
        <div className="hx-video-panel__comments" role="tabpanel">
          {comments}
        </div>
      )}
    </section>
  );
};

export default VideoPanel;
