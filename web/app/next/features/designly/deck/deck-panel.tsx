// The deck's left panel: «Чат | Заметки | Комментарии». Docked beside the slide, floating over it, or off; on a phone it
// is a bottom sheet with a handle (a tap toggles the tall sheet, a pull up makes it tall, a pull down brings it back or
// closes it, thresholds in ./panel-sheet.ts). The chat tab hosts the site chat's own feed and field; the other two are
// ./deck-notes.tsx and ./deck-comments.tsx. Mounted by the shell (WP3) with IDeckPanelProps and the deck's identity.
import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { CommentsTab } from '@/next/features/designly/deck/deck-comments';
import { NotesTab } from '@/next/features/designly/deck/deck-notes';
import type { IDeckDictation } from '@/next/features/designly/deck/dictation';
import { PANEL_DROP, panelDrop, panelFollow } from '@/next/features/designly/deck/panel-sheet';
import {
  DECK_PANEL,
  DECK_TAB,
  type IDeckContext,
  type IDeckPanelProps,
  type TDeckTab,
} from '@/next/features/designly/deck/types';
import type { IDeckComments } from '@/next/features/designly/deck/use-deck-comments';
import type { ISiteStage } from '@/next/features/designly/site-view';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';

/** The panel leaves in 170 ms, the phone's sheet in 240. */
const EXIT_MS = 170;
const SHEET_EXIT_MS = 240;

/** The chip of the chat's field and the words the agent reads with the next message. */
export const deckChatContext = (ctx: IDeckContext): { label: string; text: string; selected: boolean } => {
  const label =
    ctx.label === null
      ? t('next.deck.chat.context', { n: ctx.slide })
      : t('next.deck.chat.contextBlock', { n: ctx.slide, label: ctx.label });
  return {
    label,
    text: ctx.label === null ? `slide ${ctx.slide}` : `slide ${ctx.slide}, block «${ctx.label}»`,
    selected: ctx.label !== null,
  };
};

/** Puts the caret in the chat's field once it is there (the toolbar's «Попросить…», the phone dock's pill). */
export const focusDeckChat = (): void => {
  window.setTimeout(() => {
    document.querySelector<HTMLElement>('.hx-deck-chat textarea')?.focus({ preventScroll: true });
  }, 60);
};

const TABS: readonly { id: TDeckTab; key: string }[] = [
  { id: DECK_TAB.chat, key: 'next.deck.panel.chat' },
  { id: DECK_TAB.notes, key: 'next.deck.panel.notes' },
  { id: DECK_TAB.comments, key: 'next.deck.panel.comments' },
];

export interface IDeckPanelFullProps extends IDeckPanelProps {
  machine: MachineId;
  /** The deck's id (the project's). */
  artifact: string;
  /** The deck's one dictation (shared with the notes line under the slide and the notes sheet). */
  dict: IDeckDictation;
  comments: IDeckComments;
  /** The site stage's `context` takes the chat's chip while the panel shows the chat. */
  site?: Pick<ISiteStage, 'context'>;
}

/** The handle of the phone sheet. */
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

export const DeckPanel = ({
  mode,
  tab,
  onMode,
  onTab,
  stage,
  slideIndex,
  onGo,
  ctx,
  phone,
  machine,
  artifact,
  dict,
  comments,
  site,
}: IDeckPanelFullProps) => {
  const shown = usePresence(mode === DECK_PANEL.off ? null : mode, phone ? SHEET_EXIT_MS : EXIT_MS);
  const [panel, setPanel] = useState<HTMLElement | null>(null);
  const sheet = useSheetGrab(panel, () => onMode(DECK_PANEL.off));

  // The chat's field wears the slide (and the block) the person is on while the chat is what the panel shows.
  const chip = shown.value !== null && tab === DECK_TAB.chat && ctx ? deckChatContext(ctx) : null;
  const label = chip?.label ?? null;
  const text = chip?.text ?? null;
  const selected = chip?.selected ?? false;
  const context = useRef(site?.context);
  useLayoutEffect(() => {
    context.current = site?.context;
  });
  useEffect(() => {
    context.current?.(label === null || text === null ? null : { label, text, selected });
    return () => context.current?.(null);
  }, [label, text, selected]);

  if (shown.value === null) return null;
  const closing = shown.state === PRESENCE.closing;
  const floating = shown.value === DECK_PANEL.float;
  const open = comments.open;

  return (
    <section
      ref={setPanel}
      className="hx-deck-panel"
      data-closing={closing ? '1' : '0'}
      data-tall={sheet.tall ? '1' : '0'}
      data-drag={sheet.dragging ? '1' : '0'}
      aria-label={t('next.deck.panel.label')}
    >
      <div className="hx-deck-pgrab" aria-label={t('next.deck.panel.grab')} {...sheet.grab} />
      <div className="hx-deck-ph">
        <div className="hx-deck-seg" role="tablist" aria-label={t('next.deck.panel.label')}>
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
              {id === DECK_TAB.comments && open > 0 && <span className="hx-deck-badge">{open}</span>}
            </button>
          ))}
        </div>
        <button
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          data-deck-float=""
          type="button"
          aria-label={floating ? t('next.deck.panel.dock') : t('next.deck.panel.float')}
          title={floating ? t('next.deck.panel.dock') : t('next.deck.panel.float')}
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
          aria-label={t('next.deck.panel.hide')}
          onClick={() => onMode(DECK_PANEL.off)}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {tab === DECK_TAB.chat && (
        <div className="hx-deck-chat" role="tabpanel">
          {stage}
        </div>
      )}
      {tab === DECK_TAB.notes && (
        <NotesTab machine={machine} artifact={artifact} slideIndex={slideIndex} onGo={onGo} dict={dict} />
      )}
      {tab === DECK_TAB.comments && <CommentsTab comments={comments} onGo={onGo} />}
    </section>
  );
};
