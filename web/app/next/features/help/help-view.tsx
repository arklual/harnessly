// The documentation, /help: the contents on the left by section (above the page on a phone, one layout), the page on
// the right drawn by the chat's Markdown, so its code, tables and diagrams look as they do in a chat, and its pictures
// come from the page's own folder. The name of the page is the largest thing; nothing counts or labels. The field above
// the contents searches every page's text; the current page's headings sit under it in the contents.
import { lazy, Suspense, useMemo, type ReactNode } from 'react';
import type { HelpError, IGuideHit, IGuideSection } from '@/next/features/help/api';
import {
  filterContents,
  isOwnPicture,
  LINK,
  resolveLink,
  slugOf,
  type IGuidePlace,
} from '@/next/features/help/model';
import { t } from '@/next/i18n';
import { appBasePath } from '@/platform/config';
import { ROUTE, routePath } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/help.css';

const Markdown = lazy(() => import('@/next/features/chat/markdown'));

/** The search: nothing asked, asked and waiting, or its answer. */
export type TSearch =
  | { kind: 'idle' }
  | { kind: 'loading' }
  | { kind: 'hits'; hits: readonly IGuideHit[] }
  | { kind: 'failed'; error: HelpError };

export interface IHelpViewProps {
  /** null while the library loads. */
  sections: readonly IGuideSection[] | null;
  sectionsError: HelpError | null;
  /** The page on screen: the one the address names, else the first. */
  place: IGuidePlace | null;
  /** null while the page loads. */
  markdown: string | null;
  pageError: HelpError | null;
  query: string;
  search: TSearch;
  onQuery: (query: string) => void;
  onOpen: (section: string, page: string, anchor?: string) => void;
  /** The address of a picture of the current page. */
  pictureSrc: (src: string) => string;
  onRetry: () => void;
  onOpenDrawer: () => void;
}

const Failure = ({ error, onRetry }: { error: HelpError; onRetry: () => void }) =>
  error.network ? (
    <LoadFailed size={LOAD_FAILED_SIZE.inline} title={error.message} onRetry={onRetry} />
  ) : (
    <p className="hx-help__none">{error.message}</p>
  );

const ContentsSkeleton = () => (
  <div className="hx-help__rows" aria-busy="true">
    {[0, 1, 2, 3, 4].map((n) => (
      <Skeleton key={n} className="hx-help__sk" />
    ))}
  </div>
);

const PageSkeleton = () => (
  <div className="hx-help__article" aria-busy="true">
    <Skeleton className="hx-help__sk hx-help__sk--title" />
    <Skeleton className="hx-help__sk" />
    <Skeleton className="hx-help__sk" />
    <Skeleton className="hx-help__sk hx-help__sk--short" />
  </div>
);

const pageHref = (section: string, page: string) =>
  `${appBasePath()}${routePath({ kind: ROUTE.help, section, page })}`;

const Hits = ({
  search,
  onOpen,
  onRetry,
}: {
  search: TSearch;
  onOpen: IHelpViewProps['onOpen'];
  onRetry: () => void;
}) => {
  switch (search.kind) {
    case 'idle':
    case 'loading':
      return <ContentsSkeleton />;
    case 'failed':
      return <Failure error={search.error} onRetry={onRetry} />;
    default:
      if (search.hits.length === 0) return <p className="hx-help__none">{t('next.help.none')}</p>;
      return (
        <nav className="hx-help__rows" aria-label={t('next.help.found')}>
          {search.hits.map((hit) => (
            <a
              key={`${hit.section}/${hit.name}`}
              className="hx-help__hit"
              href={pageHref(hit.section, hit.name)}
              onClick={(event) => {
                event.preventDefault();
                onOpen(hit.section, hit.name, hit.heading ? slugOf(hit.heading) : undefined);
              }}
            >
              <span className="hx-help__hit-title">
                {hit.title}
                {hit.heading && <span className="hx-help__hit-heading"> · {hit.heading}</span>}
              </span>
              {hit.snippet && <span className="hx-help__hit-text">{hit.snippet}</span>}
            </a>
          ))}
        </nav>
      );
  }
};

const HelpView = ({
  sections,
  sectionsError,
  place,
  markdown,
  pageError,
  query,
  search,
  onQuery,
  onOpen,
  pictureSrc,
  onRetry,
  onOpenDrawer,
}: IHelpViewProps) => {
  const groups = useMemo(() => (sections ? filterContents(sections, '') : null), [sections]);
  const guide = useMemo(
    () => ({
      headingId: slugOf,
      link: (href: string, children: ReactNode): ReactNode => {
        const link = resolveLink(href, place, sections ?? []);
        switch (link.kind) {
          case LINK.page:
            return (
              <a
                href={pageHref(link.section, link.name)}
                onClick={(event) => {
                  event.preventDefault();
                  onOpen(link.section, link.name, link.anchor ?? undefined);
                }}
              >
                {children}
              </a>
            );
          case LINK.anchor:
            return (
              <a
                href={`#${link.anchor}`}
                onClick={(event) => {
                  event.preventDefault();
                  if (place) onOpen(place.section.id, place.page.name, link.anchor);
                }}
              >
                {children}
              </a>
            );
          case LINK.outside:
            return (
              <a href={link.href} target="_blank" rel="noreferrer">
                {children}
              </a>
            );
          default:
            return <span className="hx-help__plain">{children}</span>;
        }
      },
      image: (src: string, alt: string): ReactNode =>
        isOwnPicture(src) ? (
          <img className="hx-help__img" src={pictureSrc(src)} alt={alt} loading="lazy" decoding="async" />
        ) : (
          <span className="hx-help__plain">{alt}</span>
        ),
    }),
    [place, sections, onOpen, pictureSrc],
  );

  const searching = query.trim() !== '';
  let contents: ReactNode = <ContentsSkeleton />;
  if (searching) contents = <Hits search={search} onOpen={onOpen} onRetry={onRetry} />;
  else if (sectionsError) contents = <Failure error={sectionsError} onRetry={onRetry} />;
  else if (groups !== null && groups.length === 0)
    contents = <p className="hx-help__none">{t('next.help.empty')}</p>;
  else if (groups !== null) {
    contents = (
      <nav className="hx-help__rows" aria-label={t('next.help.title')}>
        {groups.map(({ section, rows }) => (
          <div key={section.id} className="hx-help__section">
            <p className="hx-help__section-title">{section.title}</p>
            {rows.map(({ page }) => {
              const current = place?.section.id === section.id && place.page.name === page.name;
              return (
                <div key={page.name} className="hx-help__group">
                  <button
                    type="button"
                    className="hx-help__row"
                    aria-current={current ? 'page' : undefined}
                    onClick={() => onOpen(section.id, page.name)}
                  >
                    {page.title}
                  </button>
                  {current &&
                    page.headings.map((heading) => (
                      <button
                        key={heading}
                        type="button"
                        className="hx-help__sub"
                        onClick={() => onOpen(section.id, page.name, slugOf(heading))}
                      >
                        {heading}
                      </button>
                    ))}
                </div>
              );
            })}
          </div>
        ))}
      </nav>
    );
  }

  let article: ReactNode = <PageSkeleton />;
  if (pageError) article = <Failure error={pageError} onRetry={onRetry} />;
  else if (markdown !== null) {
    article = (
      <article className="hx-help__article">
        <Suspense fallback={<PageSkeleton />}>
          <Markdown md={markdown} guide={guide} />
        </Suspense>
      </article>
    );
  }

  return (
    <div className="hx-help">
      <div className="hx-help__bar">
        <button
          type="button"
          className="hx-head-btn hx-appmain__menu hx-help__menu"
          aria-label={t('next.header.openMenu')}
          onClick={onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
        <h1 className="hx-help__h1">{t('next.help.title')}</h1>
      </div>
      <div className="hx-help__body">
        <aside className="hx-help__toc">
          <input
            className="hx-help__filter"
            type="search"
            value={query}
            placeholder={t('next.help.filter')}
            aria-label={t('next.help.filter')}
            autoComplete="off"
            spellCheck={false}
            onChange={(event) => onQuery(event.currentTarget.value)}
          />
          {contents}
        </aside>
        <div className="hx-help__page">{article}</div>
      </div>
    </div>
  );
};

export default HelpView;
