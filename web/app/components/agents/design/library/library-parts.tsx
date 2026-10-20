import { useEffect, useState, type ReactNode } from 'react';
import { ChevronLeft, MoreHorizontal } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  TwButton,
  TwMenuButton,
  cx,
  useTwContextMenu,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { t } from '@/lib/i18n';

interface IBackLinkProps {
  label: string;
  onClick: () => void;
}

/** The small muted way back above a page title. */
export const BackLink = ({ label, onClick }: IBackLinkProps) => (
  <button type="button" className="tw-dhome-back" onClick={onClick}>
    <ChevronLeft aria-hidden="true" />
    {label}
  </button>
);

interface ICoverProps {
  src: string | null;
  fallback: string;
}

/** A picture still being drawn on the machine is asked for again after these pauses, then given up. */
const COVER_RETRY_MS = [2_000, 4_000, 8_000, 15_000, 15_000, 15_000, 15_000, 15_000] as const;

const coverSrc = (src: string, attempt: number): string =>
  attempt === 0 ? src : `${src}${src.includes('?') ? '&' : '?'}try=${attempt}`;

/** The picture while it comes (a shimmer), then the picture; a plain cover when there is none or it never arrives. */
const Cover = ({ src, fallback }: ICoverProps) => {
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const missing = !src || failed > COVER_RETRY_MS.length;
  useEffect(() => {
    if (failed === attempt || failed > COVER_RETRY_MS.length) return;
    const timer = window.setTimeout(() => setAttempt(failed), COVER_RETRY_MS[failed - 1]);
    return () => window.clearTimeout(timer);
  }, [failed, attempt]);
  return (
    <span className="tw-dhome-cover" data-loading={(!missing && !ready) || undefined}>
      {src && !missing && failed === attempt && (
        <img
          key={attempt}
          src={coverSrc(src, attempt)}
          alt=""
          loading="lazy"
          onLoad={() => setReady(true)}
          onError={() => setFailed(attempt + 1)}
        />
      )}
      {missing && (
        <span className="tw-dhome-cover-plain" aria-hidden="true">
          {fallback}
        </span>
      )}
    </span>
  );
};

interface ICardProps {
  title: string;
  /** The muted line under the title. */
  caption?: string;
  /** The picture of the board; null, or a picture that does not come, leaves the plain cover with `fallback`. */
  src: string | null;
  fallback: string;
  onOpen: () => void;
  /** What the card can do besides open: a menu on right click, long press and the dots a pointer finds on hover. */
  menu?: readonly TTwMenuEntry[];
}

const NO_ENTRIES: readonly TTwMenuEntry[] = [];

/** A project or a board of the gallery: a cover and a name under it, no box. */
export const GalleryCard = ({ title, caption, src, fallback, onOpen, menu }: ICardProps) => {
  const context = useTwContextMenu({ items: menu ?? NO_ENTRIES, 'aria-label': title, disabled: !menu });
  return (
    <>
      <button type="button" className="tw-dhome-card" onClick={onOpen} {...(menu ? context.bind : {})}>
        <Cover key={src ?? ''} src={src} fallback={fallback} />
        <span className="tw-dhome-card-title">{title}</span>
        {caption && <span className="tw-dhome-card-caption">{caption}</span>}
      </button>
      {menu && (
        <TwMenuButton
          items={menu}
          menuLabel={title}
          align={LAYER_ALIGN.end}
          className="tw-icon-button tw-dhome-card-more"
          aria-label={t('design.libraryParts.actions', { title })}
        >
          <MoreHorizontal />
        </TwMenuButton>
      )}
      {context.menu}
    </>
  );
};

interface IGalleryGridProps {
  label: string;
  /** Show placeholders while the list loads. */
  loading?: boolean;
  children?: ReactNode;
}

const SKELETON_CARDS = 6;

export const GalleryGrid = ({ label, loading = false, children }: IGalleryGridProps) => (
  <ul className={cx('tw-dhome-grid')} aria-label={label} aria-busy={loading || undefined}>
    {loading
      ? Array.from({ length: SKELETON_CARDS }, (_, at) => (
          <li key={at} className="tw-dhome-skeleton" aria-hidden="true">
            <span className="tw-dhome-cover" data-loading />
            <span className="tw-skeleton tw-dhome-skeleton-line" />
            <span className="tw-skeleton tw-dhome-skeleton-line tw-dhome-skeleton-short" />
          </li>
        ))
      : children}
  </ul>
);

interface INoteProps {
  error?: boolean;
  children: ReactNode;
  className?: string;
}

export const Note = ({ error = false, children, className }: INoteProps) => (
  <p
    className={cx('tw-dlib-note', error && 'tw-dlib-note-error', className)}
    role={error ? 'alert' : 'status'}
  >
    {children}
  </p>
);

interface ILibraryNotFoundProps {
  /** What is gone: the line reads "Not found: perhaps <what>". */
  what: string;
  /** The way back to the projects. */
  onProjects: () => void;
  /** A nearer way back, when the project itself is there. */
  onProject?: () => void;
}

/** A link into the library that leads nowhere: calm, with a way back to the gallery. */
export const LibraryNotFound = ({ what, onProjects, onProject }: ILibraryNotFoundProps) => (
  <div className="tw-dhome-empty" role="alert">
    <strong>{t('design.libraryParts.notFound', { what })}</strong>
    <div className="tw-dhome-empty-actions">
      {onProject && (
        <TwButton size={BUTTON_SIZE.md} variant={BUTTON_VARIANT.secondary} onClick={onProject}>
          {t('design.libraryParts.toProjectBoards')}
        </TwButton>
      )}
      <TwButton size={BUTTON_SIZE.md} variant={BUTTON_VARIANT.secondary} onClick={onProjects}>
        {t('design.libraryParts.toAllProjects')}
      </TwButton>
    </div>
  </div>
);
