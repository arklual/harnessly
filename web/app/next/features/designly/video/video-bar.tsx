// The video's top bar, in the deck's bar classes (hx-deck__bar, so deck.css drops labels on a narrow window or in the
// chat's sidebar by `data-k`): back, the panel's switch, the address with the scene's crumb, the format and the source
// (live scenes or the published MP4), «Комментировать», «Стиль», «Собрать MP4» and «Поделиться».
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import ModeButton from '@/next/features/designly/mode-button';
import type { ISiteMode } from '@/next/features/designly/site-view';
import { RENDER_BADGE, type TRenderBadge } from '@/next/features/designly/video/video-model';
import { VIDEO_SOURCE, type TVideoFormat, type TVideoSource } from '@/next/features/designly/video/types';
import { ProjectDownloadMenu, type IDownloadItem } from '@/next/features/designly/project-downloads';

export interface IVideoBarProps {
  /** The switch between looking and editing; absent in the pane of a chat. */
  mode?: ISiteMode;
  /** The video is looked at: no panel, comments, style or render here. */
  viewing?: boolean;
  title: string;
  /** The place: «Сцена 3 · 0:12». */
  crumb: string;
  onBack: () => void;
  /** Absent in the chat's sidebar: the chat is the host's. */
  onPanel?: () => void;
  panelOn: boolean;
  formats: readonly TVideoFormat[];
  format: TVideoFormat;
  onFormat: (format: TVideoFormat) => void;
  source: TVideoSource;
  onSource: (source: TVideoSource) => void;
  /** The MP4 of the format on screen is published, so it can be played. */
  mp4: boolean;
  onComment: () => void;
  onStyle: () => void;
  styleOn: boolean;
  onRender: () => void;
  renderOn: boolean;
  badge: TRenderBadge;
  /** Absent: the person may not share this video. */
  onShare?: () => void;
  shareOn: boolean;
  /** «Скачать»: the MP4, the scene on screen and every scene, the project as a ZIP. */
  downloads?: readonly IDownloadItem[];
}

interface IBarButtonProps {
  k: string;
  icon: string;
  label: string;
  pressed?: boolean;
  primary?: boolean;
  disabled?: boolean;
  /** A small mark on the icon: the render is outdated. */
  mark?: boolean;
  text?: boolean;
  onClick?: () => void;
}

const BarButton = ({
  k,
  icon,
  label,
  pressed,
  primary,
  disabled,
  mark,
  text = true,
  onClick,
}: IBarButtonProps) => (
  <button
    type="button"
    className="hx-btn"
    data-variant={primary ? 'primary' : 'ghost'}
    data-size="sm"
    data-shape={text ? undefined : 'icon'}
    data-k={k}
    data-mark={mark ? '1' : undefined}
    aria-label={label}
    aria-pressed={pressed}
    disabled={disabled}
    onClick={onClick}
  >
    <Icon name={icon} />
    {text && <span className="hx-deck-lbl">{label}</span>}
  </button>
);

const VideoBar = (p: IVideoBarProps) => (
  <header className="hx-deck__bar hx-video__bar">
    <BarButton
      k="back"
      icon="arrow-left"
      label={t('next.video.shell.back')}
      text={false}
      onClick={p.onBack}
    />
    {p.onPanel && !p.viewing && (
      <BarButton
        k="panel"
        icon="panel-left"
        label={t('next.video.shell.panel')}
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
    {p.formats.length > 1 && (
      <div
        className="hx-deck-seg hx-video__seg"
        role="group"
        aria-label={t('next.video.shell.format')}
        data-k="format"
      >
        {p.formats.map((f) => (
          <button
            key={f}
            type="button"
            data-on={f === p.format ? '1' : '0'}
            aria-pressed={f === p.format}
            onClick={() => p.onFormat(f)}
          >
            {f}
          </button>
        ))}
      </div>
    )}
    <div
      className="hx-deck-seg hx-video__seg"
      role="group"
      aria-label={t('next.video.shell.source')}
      data-k="source"
    >
      <button
        type="button"
        data-on={p.source === VIDEO_SOURCE.live ? '1' : '0'}
        aria-pressed={p.source === VIDEO_SOURCE.live}
        onClick={() => p.onSource(VIDEO_SOURCE.live)}
      >
        {t('next.video.shell.live')}
      </button>
      <button
        type="button"
        data-on={p.source === VIDEO_SOURCE.mp4 ? '1' : '0'}
        aria-pressed={p.source === VIDEO_SOURCE.mp4}
        disabled={!p.mp4}
        onClick={() => p.onSource(VIDEO_SOURCE.mp4)}
      >
        {t('next.video.shell.mp4')}
      </button>
    </div>
    {p.mode && <ModeButton mode={p.mode} />}
    {!p.viewing && (
      <>
        <BarButton
          k="comment"
          icon="message-square-plus"
          label={t('next.video.shell.comment')}
          onClick={p.onComment}
        />
        <BarButton
          k="style"
          icon="palette"
          label={t('next.video.shell.style')}
          pressed={p.styleOn}
          onClick={p.onStyle}
        />
        <BarButton
          k="render"
          icon="clapperboard"
          label={t('next.video.shell.render')}
          pressed={p.renderOn}
          mark={p.badge === RENDER_BADGE.outdated}
          onClick={p.onRender}
        />
      </>
    )}
    {p.downloads && (
      <ProjectDownloadMenu
        items={p.downloads}
        trigger={
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            data-k="download"
            aria-label={t('next.download.label')}
          >
            <Icon name="download" />
          </button>
        }
      />
    )}
    <BarButton
      k="share"
      icon="link"
      label={t('next.video.shell.share')}
      pressed={p.shareOn}
      disabled={!p.onShare}
      onClick={p.onShare}
    />
  </header>
);

export default VideoBar;
