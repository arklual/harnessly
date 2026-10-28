// «Ещё» (the board's .pr-menu): the downloads — PDF, PPTX, MP4, PNG, HTML, each built by the machine as a job — and
// «Как видит гость». On a phone it also carries what the bar has no room for: the comment hint, «Чат и заметки»,
// «Стиль» and «Поделиться». An export the machine cannot build (no Chrome, no ffmpeg, nothing recorded yet) is shown
// disabled with the reason in place of its hint.
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { PRESENCE, type TPresenceState } from '@/next/ui/presence';
import { deckExportHref, startDeckExport } from '@/next/features/designly/deck/api';
import { downloadDeckFile, followDeckExport } from '@/next/features/designly/deck/exports';
import { deckToast } from '@/next/features/designly/deck/deck-toast';
import {
  DECK_EXPORTS,
  DECK_REC_STATE,
  type IDeckState,
  type TDeckExport,
} from '@/next/features/designly/deck/types';
import type { MachineId } from '@/next/protocol/v2';

const ITEMS = {
  pdf: {
    icon: 'file-text',
    name: 'next.deck.menu.pdf',
    hint: 'next.deck.menu.pdfHint',
    toast: 'next.deck.menu.pdfToast',
  },
  pptx: {
    icon: 'layers',
    name: 'next.deck.menu.pptx',
    hint: 'next.deck.menu.pptxHint',
    toast: 'next.deck.menu.pptxToast',
  },
  mp4: {
    icon: 'play',
    name: 'next.deck.menu.mp4',
    hint: 'next.deck.menu.mp4Hint',
    toast: 'next.deck.toast.videoBuilding',
  },
  png: {
    icon: 'image',
    name: 'next.deck.menu.png',
    hint: 'next.deck.menu.pngHint',
    toast: 'next.deck.menu.pngToast',
  },
  html: {
    icon: 'download',
    name: 'next.deck.menu.html',
    hint: 'next.deck.menu.htmlHint',
    toast: 'next.deck.menu.htmlToast',
  },
} as const satisfies Record<TDeckExport, { icon: string; name: string; hint: string; toast: string }>;

/** The newest finished recording: what the MP4 is made of. */
export const lastRecording = (deck: IDeckState): string | null =>
  [...deck.recordings].reverse().find((r) => r.state === DECK_REC_STATE.done)?.id ?? null;

/** Why an export cannot be built on this machine now; null when it can. */
export const exportBlocked = (deck: IDeckState, format: TDeckExport): string | null => {
  if (!deck.chrome) return t('next.deck.menu.needsChrome');
  if (format !== 'mp4') return null;
  if (!deck.ffmpeg) return t('next.deck.menu.needsFfmpeg');
  return lastRecording(deck) === null ? t('next.deck.menu.noRecording') : null;
};

/**
 * Builds an export and follows its job (deck/exports.ts): the deck's toast says it started, a failure says why, and
 * the file is downloaded when it is done (for an MP4 that download is the promised notice).
 */
export const exportDeck = async (
  machine: MachineId,
  artifact: string,
  format: TDeckExport,
  rec?: string,
): Promise<void> => {
  try {
    const first = await startDeckExport(machine, artifact, format, rec);
    deckToast(t(ITEMS[format].toast));
    const job = await followDeckExport(machine, artifact, first);
    downloadDeckFile(deckExportHref(machine, artifact, job.id));
  } catch (failure) {
    toast(t('next.deck.menu.failed'), {
      tone: 'error',
      description: failure instanceof Error ? failure.message : undefined,
    });
  }
};

export interface IDeckMenuProps {
  state: TPresenceState;
  /** null while the deck is not read yet: the downloads wait for it. */
  deck: IDeckState | null;
  /** Edit: the phone's section has the panel, the style and the share. */
  edit: boolean;
  onExport: (format: TDeckExport) => void;
  onGuest: () => void;
  onPanel: () => void;
  onStyle: () => void;
  onShare?: () => void;
}

const DeckMenu = ({ state, deck, edit, onExport, onGuest, onPanel, onStyle, onShare }: IDeckMenuProps) => (
  <div
    className="hx-deck-menu"
    data-closing={state === PRESENCE.closing ? '1' : undefined}
    role="menu"
    aria-label={t('next.deck.bar.more')}
  >
    <div className="hx-deck-menu__mob">
      <div className="hx-deck-menu__hint">{t('next.deck.menu.phoneHint')}</div>
      {edit && (
        <>
          <button type="button" className="hx-deck-item" role="menuitem" onClick={onPanel}>
            <Icon name="panel-left" />
            <span>{t('next.deck.menu.chatNotes')}</span>
          </button>
          <button type="button" className="hx-deck-item" role="menuitem" onClick={onStyle}>
            <Icon name="palette" />
            <span>{t('next.deck.bar.style')}</span>
          </button>
          <button
            type="button"
            className="hx-deck-item"
            role="menuitem"
            disabled={!onShare}
            onClick={onShare}
          >
            <Icon name="link" />
            <span>{t('next.deck.bar.share')}</span>
          </button>
        </>
      )}
      <div className="hx-deck-menu__sep" />
    </div>
    <div className="hx-deck-menu__h">{t('next.deck.menu.download')}</div>
    {DECK_EXPORTS.map((format) => {
      const item = ITEMS[format];
      const blocked = deck === null ? null : exportBlocked(deck, format);
      return (
        <button
          key={format}
          type="button"
          className="hx-deck-item"
          role="menuitem"
          data-format={format}
          disabled={deck === null || blocked !== null}
          onClick={() => onExport(format)}
        >
          <Icon name={item.icon} />
          <span>
            {t(item.name)}
            <small>{blocked ?? t(item.hint)}</small>
          </span>
        </button>
      );
    })}
    <div className="hx-deck-menu__sep" />
    <button type="button" className="hx-deck-item" role="menuitem" onClick={onGuest}>
      <Icon name="eye" />
      <span>{t('next.deck.menu.asGuest')}</span>
    </button>
  </div>
);

export default DeckMenu;
