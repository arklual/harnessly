// «Стиль» (the board's .pr-pop): the deck's theme and font, written to the deck's settings (the runtime rewrites
// deck-theme.css and every board redraws), and the layout of the slide on screen, which is the agent's to redo: a pick
// asks it in the chat and marks the choice. The footer names the company template the theme came from.
import { useLayoutEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { PRESENCE, type TPresenceState } from '@/next/ui/presence';
import {
  DECK_FONT,
  DECK_FONT_CSS,
  DECK_LAYOUT,
  type IDeckSettings,
  type IDeckTheme,
  type TDeckFont,
  type TDeckLayout,
} from '@/next/features/designly/deck/types';

/** The built-in themes (internal/design/deck_types.go, DefaultDeckThemes); their names are the page's own words. */
export const DECK_THEMES: readonly IDeckTheme[] = [
  {
    id: 'co',
    name: 'next.deck.style.themeCompany',
    vars: {
      '--deck-bg': '#ffffff',
      '--deck-ink': '#1c1c1f',
      '--deck-mute': '#4a4a52',
      '--deck-acc': '#ffdd2d',
      '--deck-on-acc': '#1c1c1f',
      '--deck-acc-soft': '#fff6c2',
      '--deck-acc-ink': '#1c1c1f',
      '--deck-panel': '#f4f4f6',
      '--deck-line': '#d9d9de',
    },
  },
  {
    id: 'vi',
    name: 'next.deck.style.themeViolet',
    vars: {
      '--deck-bg': '#fbfaff',
      '--deck-ink': '#16161a',
      '--deck-mute': '#3d3d46',
      '--deck-acc': '#7b61ff',
      '--deck-on-acc': '#ffffff',
      '--deck-acc-soft': '#f1eeff',
      '--deck-acc-ink': '#6a4ff0',
      '--deck-panel': '#f4f2fc',
      '--deck-line': '#cfc8f5',
    },
  },
  {
    id: 'gr',
    name: 'next.deck.style.themeForest',
    vars: {
      '--deck-bg': '#f7f6f1',
      '--deck-ink': '#17221d',
      '--deck-mute': '#3f4a44',
      '--deck-acc': '#1f6b4f',
      '--deck-on-acc': '#ffffff',
      '--deck-acc-soft': '#e2efe8',
      '--deck-acc-ink': '#1f6b4f',
      '--deck-panel': '#ecebe4',
      '--deck-line': '#c9d6cf',
    },
  },
];

/** The deck's themes: its own, or the built-in ones with their names in the page's language. */
export const deckThemes = (settings: IDeckSettings): readonly IDeckTheme[] =>
  settings.themes && settings.themes.length > 0
    ? settings.themes
    : DECK_THEMES.map((theme) => ({ ...theme, name: t(theme.name) }));

/** The theme in use; the first one when the settings name one the deck does not have. */
export const deckTheme = (settings: IDeckSettings): IDeckTheme => {
  const themes = deckThemes(settings);
  return themes.find((theme) => theme.id === settings.theme) ?? themes[0];
};

const FONTS = [
  { id: DECK_FONT.inter, key: 'next.deck.style.fontInter' },
  { id: DECK_FONT.golos, key: 'next.deck.style.fontGolos' },
  { id: DECK_FONT.serif, key: 'next.deck.style.fontSerif' },
] as const;

const LAYOUTS = [
  { id: DECK_LAYOUT.cover, key: 'next.deck.style.layoutCover' },
  { id: DECK_LAYOUT.statement, key: 'next.deck.style.layoutStatement' },
  { id: DECK_LAYOUT.list, key: 'next.deck.style.layoutList' },
  { id: DECK_LAYOUT.cards, key: 'next.deck.style.layoutCards' },
] as const;

/** Writes a theme's variables on an element (a swatch, the deck's root). */
export const useThemeVars = <T extends HTMLElement>(
  vars: Record<string, string> | null,
  font?: TDeckFont,
) => {
  const ref = useRef<T>(null);
  const key = JSON.stringify([vars, font]);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const [v, f] = JSON.parse(key) as [Record<string, string> | null, TDeckFont | undefined];
    for (const [name, value] of Object.entries(v ?? {})) {
      if (name.startsWith('--deck-')) el.style.setProperty(name, value);
    }
    if (f) el.style.setProperty('--deck-font', DECK_FONT_CSS[f]);
  }, [key]);
  return ref;
};

const Swatch = ({ theme, on, onPick }: { theme: IDeckTheme; on: boolean; onPick: () => void }) => {
  const ref = useThemeVars<HTMLButtonElement>(theme.vars);
  return (
    <button
      ref={ref}
      type="button"
      className="hx-deck-swb"
      data-on={on ? '1' : '0'}
      aria-pressed={on}
      onClick={onPick}
    >
      <i />
      {theme.name}
    </button>
  );
};

export interface IDeckStyleProps {
  state: TPresenceState;
  /** null while the deck is not read yet. */
  settings: IDeckSettings | null;
  /** Saves the settings; a refusal is thrown with its reason. */
  onSettings: (change: Partial<IDeckSettings>) => Promise<void>;
  /** The layout asked of the agent for the slide on screen, if any. */
  layout: TDeckLayout | null;
  onLayout: (layout: TDeckLayout) => void;
}

const DeckStyle = ({ state, settings, onSettings, layout, onLayout }: IDeckStyleProps) => {
  // The pick under way: shown as chosen until the runtime has it (or refuses).
  const [pending, setPending] = useState<Partial<IDeckSettings> | null>(null);
  const change = (next: Partial<IDeckSettings>) => {
    if (pending) return;
    setPending(next);
    onSettings(next).then(
      () => setPending(null),
      (failure: Error) => {
        setPending(null);
        toast(t('next.deck.style.failed'), { tone: 'error', description: failure.message });
      },
    );
  };
  const theme = settings ? (pending?.theme ?? settings.theme) : null;
  const font = settings ? (pending?.font ?? settings.font) : null;
  const themes = settings ? deckThemes(settings) : DECK_THEMES.map((th) => ({ ...th, name: t(th.name) }));
  const source = settings?.style?.source ?? '';
  return (
    <div
      className="hx-deck-pop hx-deck-style"
      data-closing={state === PRESENCE.closing ? '1' : undefined}
      role="dialog"
      aria-label={t('next.deck.style.label')}
      aria-busy={settings === null || pending !== null || undefined}
    >
      <div className="hx-deck-h">{t('next.deck.style.theme')}</div>
      <div className="hx-deck-sws">
        {themes.map((th) => (
          <Swatch
            key={th.id}
            theme={th}
            on={th.id === theme}
            onPick={() => settings && change({ theme: th.id })}
          />
        ))}
      </div>
      <div className="hx-deck-h">{t('next.deck.style.font')}</div>
      <div className="hx-deck-seg">
        {FONTS.map((f) => (
          <button
            key={f.id}
            type="button"
            data-font={f.id}
            data-on={f.id === font ? '1' : '0'}
            aria-pressed={f.id === font}
            onClick={() => settings && change({ font: f.id })}
          >
            {t(f.key)}
          </button>
        ))}
      </div>
      <div className="hx-deck-h">{t('next.deck.style.layout')}</div>
      <div className="hx-deck-lays">
        {LAYOUTS.map((l) => (
          <button
            key={l.id}
            type="button"
            className="hx-deck-lay"
            data-on={l.id === layout ? '1' : '0'}
            aria-pressed={l.id === layout}
            onClick={() => onLayout(l.id)}
          >
            <div className="hx-deck-lay__mini" data-kind={l.id}>
              <i>{t('next.deck.style.layoutGlyph')}</i>
              <u />
              <s>
                <em />
                <em />
                <em />
              </s>
            </div>
            {t(l.key)}
          </button>
        ))}
      </div>
      {source !== '' && settings && (
        <div className="hx-deck-from">
          <Icon name="layers" size="sm" />
          {t('next.deck.style.from', { theme: deckTheme(settings).name, source })}
        </div>
      )}
    </div>
  );
};

export default DeckStyle;
