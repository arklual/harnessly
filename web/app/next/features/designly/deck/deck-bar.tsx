// The deck's top bar (the board's .pr-bar): back, the panel's switch, the address with the slide's crumb, the screen's
// own actions and «Ещё». Which buttons a narrow window or the chat's sidebar drops is the stylesheet's (deck.css, by
// `data-k`), so one bar serves every width. It slides away in the show and the recording.
import type { ReactNode } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';
import {
  DECK_OPENS,
  DECK_SCREEN,
  type TDeckOpens,
  type TDeckScreen,
} from '@/next/features/designly/deck/types';

export interface IDeckBarProps {
  screen: TDeckScreen;
  title: string;
  /** The screen's place: the slide's title, «Запись», «3 из 5». */
  crumb: string;
  onBack: () => void;
  /** Edit: the left panel's switch. */
  onPanel: () => void;
  panelOn: boolean;
  onComment: () => void;
  onStyle: () => void;
  styleOn: boolean;
  onShow: () => void;
  onRec: () => void;
  /** Why «Показать» and «Записать» cannot start now (no slides yet, the agent is making them); null when they can. */
  playBlocked: string | null;
  /** Absent: the person may not share this deck. */
  onShare?: () => void;
  shareOn: boolean;
  onMore: () => void;
  moreOn: boolean;
  /** Link: slides or video, and the full screen. */
  lmode: TDeckOpens;
  onLmode: (mode: TDeckOpens) => void;
  onFull: () => void;
  /** Montage: the video's link and its MP4. */
  onVideoLink: () => void;
  onDownloadMp4: () => void;
}

const Kbd = ({ children }: { children: ReactNode }) => <span className="hx-deck-kbd">{children}</span>;

interface IBarButtonProps {
  k: string;
  icon?: string;
  label: string;
  /** Shown beside the icon on a wide window; the label is the name either way. */
  text?: boolean;
  kbd?: string;
  primary?: boolean;
  /** The red dot of «Записать» instead of an icon. */
  dot?: boolean;
  pressed?: boolean;
  disabled?: boolean;
  /** Unavailable for this reason: the button stays hoverable (aria-disabled) so the reason shows as its hint. */
  blocked?: string | null;
  onClick?: () => void;
}

const BarButton = ({
  k,
  icon,
  label,
  text = true,
  kbd,
  primary,
  dot,
  pressed,
  disabled,
  blocked = null,
  onClick,
}: IBarButtonProps) => {
  const button = (
    <button
      type="button"
      className="hx-btn"
      data-variant={primary ? 'primary' : 'ghost'}
      data-size="sm"
      data-shape={text ? undefined : 'icon'}
      data-k={k}
      aria-label={label}
      aria-pressed={pressed}
      aria-disabled={blocked !== null ? true : undefined}
      disabled={disabled}
      onClick={blocked !== null ? undefined : onClick}
    >
      {dot && <span className="hx-deck-dot" />}
      {icon && <Icon name={icon} />}
      {text && <span className="hx-deck-lbl">{label}</span>}
      {kbd && <Kbd>{kbd}</Kbd>}
    </button>
  );
  return blocked !== null ? <Tooltip label={blocked}>{button}</Tooltip> : button;
};

const DeckBar = (p: IDeckBarProps) => {
  const edit = p.screen === DECK_SCREEN.edit;
  const link = p.screen === DECK_SCREEN.link;
  const cut = p.screen === DECK_SCREEN.cut;
  return (
    <header className="hx-deck__bar">
      <BarButton k="back" icon="arrow-left" label={t('next.deck.bar.back')} text={false} onClick={p.onBack} />
      {edit && (
        <BarButton
          k="panel"
          icon="panel-left"
          label={t('next.deck.bar.panel')}
          text={false}
          pressed={p.panelOn}
          onClick={p.onPanel}
        />
      )}
      <div className="hx-deck__addr">
        <b>{p.title}</b>
        {p.crumb !== '' && (
          <>
            <span>/</span>
            <em className="hx-deck__crumb">{p.crumb}</em>
          </>
        )}
      </div>
      <span className="hx-deck-grow" />
      {edit && (
        <>
          <BarButton
            k="comment"
            icon="message-square-plus"
            label={t('next.deck.bar.comment')}
            kbd="C"
            onClick={p.onComment}
          />
          <BarButton
            k="style"
            icon="palette"
            label={t('next.deck.bar.style')}
            pressed={p.styleOn}
            onClick={p.onStyle}
          />
          <BarButton
            k="show"
            icon="play"
            label={t('next.deck.bar.show')}
            kbd="P"
            blocked={p.playBlocked}
            onClick={p.onShow}
          />
          <BarButton
            k="rec"
            label={t('next.deck.bar.record')}
            kbd="R"
            primary
            dot
            blocked={p.playBlocked}
            onClick={p.onRec}
          />
          <BarButton
            k="share"
            icon="link"
            label={t('next.deck.bar.share')}
            pressed={p.shareOn}
            disabled={!p.onShare}
            onClick={p.onShare}
          />
        </>
      )}
      {link && (
        <>
          <div className="hx-deck-seg hx-deck__lmode" role="group">
            <button
              type="button"
              data-on={p.lmode !== DECK_OPENS.video ? '1' : '0'}
              aria-pressed={p.lmode !== DECK_OPENS.video}
              onClick={() => p.onLmode(DECK_OPENS.slides)}
            >
              {t('next.deck.bar.slides')}
            </button>
            <button
              type="button"
              data-on={p.lmode === DECK_OPENS.video ? '1' : '0'}
              aria-pressed={p.lmode === DECK_OPENS.video}
              onClick={() => p.onLmode(DECK_OPENS.video)}
            >
              {t('next.deck.bar.video')}
            </button>
          </div>
          <BarButton
            k="comment"
            icon="message-square-plus"
            label={t('next.deck.bar.comment')}
            kbd="C"
            onClick={p.onComment}
          />
          <BarButton
            k="full"
            icon="maximize-2"
            label={t('next.deck.bar.full')}
            kbd="F"
            primary
            onClick={p.onFull}
          />
        </>
      )}
      {cut && (
        <>
          <BarButton k="vlink" icon="link" label={t('next.deck.bar.link')} onClick={p.onVideoLink} />
          <BarButton
            k="mp4"
            icon="download"
            label={t('next.deck.bar.downloadMp4')}
            primary
            onClick={p.onDownloadMp4}
          />
        </>
      )}
      <BarButton
        k="more"
        icon="ellipsis"
        label={t('next.deck.bar.more')}
        text={false}
        pressed={p.moreOn}
        onClick={p.onMore}
      />
    </header>
  );
};

export default DeckBar;
