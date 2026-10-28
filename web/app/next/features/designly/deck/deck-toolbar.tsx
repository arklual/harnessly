// The tools of the text being edited (the board's .pr-tb): font, size, bold, italic and colour written into the board
// as the block's style, «✦ Короче» asked of the agent, and «Попросить…» (in the chat's sidebar «В чат»), which takes
// the block to the chat. On a wide window they stand above the block; on a phone the same tools are a row of the edit
// sheet (deck-sheets.tsx). The font and size lists drop down under their buttons.
import { useLayoutEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence, PRESENCE } from '@/next/ui/presence';
import type { TDeckRect } from '@/next/features/designly/deck/bridge';
import { DECK_FONT, DECK_FONT_CSS, type TDeckFont } from '@/next/features/designly/deck/types';

/** The toolbar stands this far above the block, px. */
const TOOLBAR_GAP = 52;
const MENU_EXIT_MS = 160;

/** The text sizes offered, board px. */
export const DECK_TEXT_SIZES = [14, 16, 19, 22, 24, 28, 32, 40, 44, 48, 56, 64, 72, 88, 96, 120] as const;

/** The colours a text may take: the theme's own, so a theme change recolours it. */
export const DECK_TEXT_COLORS = [
  { id: 'ink', css: 'var(--deck-ink)', key: 'next.deck.toolbar.colorInk' },
  { id: 'mute', css: 'var(--deck-mute)', key: 'next.deck.toolbar.colorMute' },
  { id: 'acc', css: 'var(--deck-acc)', key: 'next.deck.toolbar.colorAccent' },
  { id: 'accInk', css: 'var(--deck-acc-ink)', key: 'next.deck.toolbar.colorAccentInk' },
] as const;

const FONT_NAMES = {
  inter: 'next.deck.style.fontInter',
  golos: 'next.deck.style.fontGolos',
  serif: 'next.deck.style.fontSerif',
} as const satisfies Record<TDeckFont, string>;

/** The nearest size offered to a measured one. */
export const nearestSize = (size: number): number =>
  DECK_TEXT_SIZES.reduce(
    (best, s) => (Math.abs(s - size) < Math.abs(best - size) ? s : best),
    DECK_TEXT_SIZES[0],
  );

/** The selected text, as the board said it. */
export interface IDeckSelection {
  tpl: number;
  label: string;
  kind: 'title' | 'text';
  /** Its font size, board px; 0 when unknown. */
  size: number;
  rect: TDeckRect;
  /** The board's fingerprint of the element: a style patch carries it. */
  was: string;
}

/** What the person set on a block in this session: Ж and К switch back off. */
export interface IDeckTextLook {
  font?: TDeckFont;
  size?: number;
  bold?: boolean;
  italic?: boolean;
  color?: string | null;
}

export interface IDeckTextToolsProps {
  sel: IDeckSelection;
  look: IDeckTextLook;
  /** A CSS property of the block, null removes it. */
  onStyle: (prop: string, value: string | null) => void;
  onShorter: () => void;
}

const MENU = { font: 'font', size: 'size', color: 'color' } as const;
type TMenu = (typeof MENU)[keyof typeof MENU];

/** Font ▾, size ▾, Ж, К, colour and «✦ Короче»: the bar's and the edit sheet's row. */
export const DeckTextTools = ({
  sel,
  look,
  onStyle,
  onShorter,
  sheet,
}: IDeckTextToolsProps & { sheet?: boolean }) => {
  const [menu, setMenu] = useState<TMenu | null>(null);
  const shown = usePresence(menu, MENU_EXIT_MS);
  const font = look.font ?? DECK_FONT.inter;
  const size = look.size ?? nearestSize(sel.size || (sel.kind === 'title' ? 44 : 19));
  const toggle = (m: TMenu) => setMenu((now) => (now === m ? null : m));
  const pick = (prop: string, value: string | null) => {
    onStyle(prop, value);
    setMenu(null);
  };
  return (
    <>
      <button
        type="button"
        data-k="font"
        aria-haspopup="menu"
        aria-expanded={menu === MENU.font}
        onClick={() => toggle(MENU.font)}
      >
        {t(FONT_NAMES[font])}
        <Icon name="chevron-down" size="sm" />
      </button>
      <button
        type="button"
        data-k="size"
        aria-haspopup="menu"
        aria-expanded={menu === MENU.size}
        aria-label={t('next.deck.toolbar.size')}
        onClick={() => toggle(MENU.size)}
      >
        {size}
        <Icon name="chevron-down" size="sm" />
      </button>
      {!sheet && <i className="hx-deck-tb__sep" />}
      <button
        type="button"
        className="hx-deck-tb__b"
        aria-label={t('next.deck.toolbar.bold')}
        aria-pressed={look.bold === true}
        onClick={() => onStyle('font-weight', look.bold ? null : '700')}
      >
        {t('next.deck.toolbar.boldGlyph')}
      </button>
      <button
        type="button"
        className="hx-deck-tb__i"
        aria-label={t('next.deck.toolbar.italic')}
        aria-pressed={look.italic === true}
        onClick={() => onStyle('font-style', look.italic ? null : 'italic')}
      >
        {t('next.deck.toolbar.italicGlyph')}
      </button>
      <button
        type="button"
        aria-label={t('next.deck.toolbar.color')}
        aria-haspopup="menu"
        aria-expanded={menu === MENU.color}
        onClick={() => toggle(MENU.color)}
      >
        <span
          className="hx-deck-sw"
          data-c={DECK_TEXT_COLORS.find((c) => c.css === look.color)?.id ?? 'acc'}
        />
      </button>
      {!sheet && <i className="hx-deck-tb__sep" />}
      <button type="button" className="hx-deck-tb__ai" onClick={onShorter}>
        <Icon name="sparkles" size="sm" />
        {t('next.deck.toolbar.shorter')}
      </button>
      {shown.value !== null && (
        <div
          className="hx-deck-tb__menu"
          data-m={shown.value}
          data-closing={shown.state === PRESENCE.closing ? '1' : undefined}
          role="menu"
        >
          {shown.value === MENU.font &&
            Object.values(DECK_FONT).map((id) => (
              <button
                key={id}
                type="button"
                role="menuitemradio"
                aria-checked={font === id}
                data-font={id}
                onClick={() => pick('font-family', DECK_FONT_CSS[id])}
              >
                {t(FONT_NAMES[id])}
              </button>
            ))}
          {shown.value === MENU.size &&
            DECK_TEXT_SIZES.map((s) => (
              <button
                key={s}
                type="button"
                role="menuitemradio"
                aria-checked={size === s}
                onClick={() => pick('font-size', `${s}px`)}
              >
                {s}
              </button>
            ))}
          {shown.value === MENU.color && (
            <>
              {DECK_TEXT_COLORS.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  role="menuitemradio"
                  aria-checked={look.color === c.css}
                  onClick={() => pick('color', c.css)}
                >
                  <span className="hx-deck-sw" data-c={c.id} />
                  {t(c.key)}
                </button>
              ))}
              <button
                type="button"
                role="menuitemradio"
                aria-checked={!look.color}
                onClick={() => pick('color', null)}
              >
                <Icon name="undo-2" size="sm" />
                {t('next.deck.toolbar.colorReset')}
              </button>
            </>
          )}
        </div>
      )}
    </>
  );
};

export interface IDeckToolbarProps extends IDeckTextToolsProps {
  /** The ask's button: «Попросить…», or «В чат» in the chat's sidebar. */
  askLabel: string;
  onAsk: () => void;
  /** A press on the toolbar: the text loses the focus to it, and that must not end the selection. */
  onHold: () => void;
}

/** Above the block on a wide window; kept inside the slot's width. */
const DeckToolbar = ({ askLabel, onAsk, onHold, ...tools }: IDeckToolbarProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const [x, y] = tools.sel.rect;
  // The block's corner in board px and the toolbar's own width: the stylesheet scales the one by the slot's
  // `--deck-k` and keeps the toolbar inside the slot with the other, so a resize needs no render.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.setProperty('--x', String(x));
    el.style.setProperty('--y', String(y));
    el.style.setProperty('--tb-w', `${el.offsetWidth}px`);
    el.style.setProperty('--tb-gap', `${TOOLBAR_GAP}px`);
  }, [x, y]);
  return (
    <div
      ref={ref}
      className="hx-deck-tb"
      role="toolbar"
      aria-label={t('next.deck.toolbar.label')}
      onPointerDown={onHold}
    >
      <DeckTextTools {...tools} />
      <button type="button" className="hx-deck-tb__ai" onClick={onAsk}>
        {askLabel}
      </button>
    </div>
  );
};

export default DeckToolbar;
