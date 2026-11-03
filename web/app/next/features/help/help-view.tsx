// The documentation, /help: the contents on the left (above the page on a phone, one layout), the page on the right drawn
// by the chat's Markdown, so its code, tables and diagrams look as they do in a chat. The name of the page is the largest
// thing; nothing counts or labels. A filter over the contents looks at titles and headings.
import { lazy, Suspense, useMemo, useState, type ReactNode } from 'react';
import type { HelpError, IGuidePage } from '@/next/features/help/api';
import { filterContents, LINK, resolveLink, slugOf } from '@/next/features/help/model';
import { t } from '@/next/i18n';
import { appBasePath } from '@/platform/config';
import { ROUTE, routePath } from '@/next/routes/routes';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/help.css';

const Markdown = lazy(() => import('@/next/features/chat/markdown'));

export interface IHelpViewProps {
  /** null while the list loads. */
  pages: readonly IGuidePage[] | null;
  pagesError: HelpError | null;
  /** The page on screen: the one the address names, else the first. */
  current: string | null;
  /** null while the page loads. */
  markdown: string | null;
  pageError: HelpError | null;
  onOpen: (page: string, anchor?: string) => void;
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

const HelpView = ({
  pages,
  pagesError,
  current,
  markdown,
  pageError,
  onOpen,
  onRetry,
  onOpenDrawer,
}: IHelpViewProps) => {
  const [query, setQuery] = useState('');
  const rows = useMemo(() => (pages ? filterContents(pages, query) : null), [pages, query]);
  const guide = useMemo(
    () => ({
      headingId: slugOf,
      link: (href: string, children: ReactNode): ReactNode => {
        const link = resolveLink(href, pages ?? []);
        switch (link.kind) {
          case LINK.page:
            return (
              <a
                href={`${appBasePath()}${routePath({ kind: ROUTE.help, page: link.name })}`}
                onClick={(event) => {
                  event.preventDefault();
                  onOpen(link.name, link.anchor ?? undefined);
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
                  if (current) onOpen(current, link.anchor);
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
    }),
    [pages, current, onOpen],
  );

  let contents: ReactNode = <ContentsSkeleton />;
  if (pagesError) contents = <Failure error={pagesError} onRetry={onRetry} />;
  else if (rows !== null && rows.length === 0)
    contents = <p className="hx-help__none">{t('next.help.none')}</p>;
  else if (rows !== null) {
    contents = (
      <nav className="hx-help__rows" aria-label={t('next.help.title')}>
        {rows.map(({ page, headings }) => (
          <div key={page.name} className="hx-help__group">
            <button
              type="button"
              className="hx-help__row"
              aria-current={page.name === current ? 'page' : undefined}
              onClick={() => onOpen(page.name)}
            >
              {page.title}
            </button>
            {headings.map((heading) => (
              <button
                key={heading}
                type="button"
                className="hx-help__sub"
                onClick={() => onOpen(page.name, slugOf(heading))}
              >
                {heading}
              </button>
            ))}
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
            onChange={(event) => setQuery(event.currentTarget.value)}
          />
          {contents}
        </aside>
        <div className="hx-help__page">{article}</div>
      </div>
    </div>
  );
};

export default HelpView;
