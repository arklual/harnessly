// «Ещё» (the board's .pr-menu): «Скачать…» (the bar's download menu, deckDownloads: PDF, PPTX, MP4, PNG, HTML and
// the whole project, each built by the machine as a job) and «Как видит гость». On a phone it also carries what the
// bar has no room for: the comment hint, «Чат и заметки», «Стиль» and «Поделиться». An export the machine cannot
// build (no Chrome, no ffmpeg, nothing recorded yet) is shown disabled with the reason in place of its hint.
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { PRESENCE, type TPresenceState } from '@/next/ui/presence';
import { deckExportHref, startDeckExport } from '@/next/features/designly/deck/api';
import { downloadDeckFile, followDeckExport } from '@/next/features/designly/deck/exports';
import { deckToast } from '@/next/features/designly/deck/deck-toast';
import {
  DECK_ARCHIVES,
  DECK_EXPORTS,
  DECK_REC_STATE,
  type IDeckState,
  type TDeckExport,
} from '@/next/features/designly/deck/types';
import type { MachineId } from '@/next/protocol/v2';
import type { IDownloadItem } from '@/next/features/designly/project-downloads';

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
  bundle: {
    icon: 'files',
    name: 'next.deck.menu.bundle',
    hint: 'next.deck.menu.bundleHint',
    toast: 'next.deck.menu.bundleToast',
  },
} as const satisfies Record<TDeckExport, { icon: string; name: string; hint: string; toast: string }>;

/** The newest finished recording: what the MP4 is made of. */
export const lastRecording = (deck: IDeckState): string | null =>
  [...deck.recordings].reverse().find((r) => r.state === DECK_REC_STATE.done)?.id ?? null;

/** Why an export cannot be built on this machine now; null when it can. */
export const exportBlocked = (deck: IDeckState, format: TDeckExport): string | null => {
  if (DECK_ARCHIVES.has(format)) return null;
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
  /** Edit: the phone's section has the panel, the style and the share. */
  edit: boolean;
  /** «Скачать…»: the bar's download menu. */
  onDownload: () => void;
  onGuest: () => void;
  onPanel: () => void;
  onStyle: () => void;
  onShare?: () => void;
}

/**
 * The deck's downloads for «Скачать» on its bar (project-downloads.tsx draws them): each format with what it is, or
 * why the machine cannot make it now. The whole project is offered to those who may change it.
 */
export const deckDownloads = (
  deck: IDeckState | null,
  archives: boolean,
  onExport: (format: TDeckExport) => void,
): IDownloadItem[] =>
  DECK_EXPORTS.filter((format) => archives || !DECK_ARCHIVES.has(format)).map((format) => {
    const item = ITEMS[format];
    const blocked = deck === null ? null : exportBlocked(deck, format);
    return {
      id: format,
      icon: item.icon,
      label: t(item.name),
      hint: blocked ?? t(item.hint),
      disabled: deck === null || blocked !== null,
      sep: DECK_ARCHIVES.has(format),
      pick: () => onExport(format),
    };
  });

const DeckMenu = ({ state, edit, onDownload, onGuest, onPanel, onStyle, onShare }: IDeckMenuProps) => (
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
    <button type="button" className="hx-deck-item" role="menuitem" onClick={onDownload}>
      <Icon name="download" />
      <span>{t('next.download.more')}</span>
    </button>
    <button type="button" className="hx-deck-item" role="menuitem" onClick={onGuest}>
      <Icon name="eye" />
      <span>{t('next.deck.menu.asGuest')}</span>
    </button>
  </div>
);

export default DeckMenu;
