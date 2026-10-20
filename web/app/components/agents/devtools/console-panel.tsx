import { useMemo, useState, type ReactNode } from 'react';
import { ChevronRight, CircleX, Info, TriangleAlert } from 'lucide-react';
import { CHIP_TONE, TwBadge, cx } from '@/components/agents/ui';
import type { IConsoleEntry, TConsoleSource } from '@/lib/hl-preview/devtools/types';
import {
  CONSOLE_FILTER,
  filterConsole,
  formatClock,
  tail,
  type TConsoleFilter,
} from '@/lib/hl-preview-devtools-view';
import type { TPreviewDevtools } from '@/hooks/use-preview-devtools';
import { useStickToBottom } from '@/components/agents/devtools/use-stick-to-bottom';
import { plural, t } from '@/lib/i18n';

const LEVEL_ICONS = { error: CircleX, warn: TriangleAlert, info: Info, log: null } as const;

const SOURCE_LABELS: Record<TConsoleSource, string | null> = {
  console: null,
  exception: t('devtools.consolePanel.exception'),
  rejection: t('devtools.consolePanel.rejection'),
  network: t('devtools.consolePanel.network'),
};

const FILTER_LABELS: Record<TConsoleFilter, string> = {
  errors: t('devtools.consolePanel.errors'),
  warnings: t('devtools.consolePanel.warnings'),
  all: t('devtools.consolePanel.all'),
};

/** The page's address without the slot's origin noise: path and query. */
const pagePath = (url: string | null): string | null => {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    return `${parsed.host}${parsed.pathname === '/' ? '' : parsed.pathname}`;
  } catch {
    return url;
  }
};

const Entry = ({ entry }: { entry: IConsoleEntry }) => {
  const [open, setOpen] = useState(false);
  const Icon = LEVEL_ICONS[entry.level];
  const source = SOURCE_LABELS[entry.source];
  const page = pagePath(entry.url);
  const expandable = entry.stack !== null || entry.text.includes('\n') || entry.text.length > 160;
  return (
    <li className="tw-devtools-log" data-level={entry.level}>
      <button
        type="button"
        className="tw-devtools-log-line"
        aria-expanded={expandable ? open : undefined}
        disabled={!expandable}
        onClick={() => setOpen(!open)}
      >
        <span className="tw-devtools-log-icon" aria-hidden>
          {expandable ? (
            <ChevronRight className={cx('tw-devtools-chevron', open && 'tw-devtools-chevron-open')} />
          ) : null}
          {Icon ? <Icon /> : null}
        </span>
        {entry.count > 1 && (
          <TwBadge
            tone={entry.level === 'error' ? CHIP_TONE.danger : CHIP_TONE.neutral}
            className="tw-devtools-log-count"
            aria-label={plural(entry.count, 'devtools.consolePanel.repeated')}
          >
            {entry.count}
          </TwBadge>
        )}
        <span className={cx('tw-devtools-log-text', open && 'tw-devtools-log-text-open')}>
          {source && <span className="tw-devtools-log-source">{source}: </span>}
          {entry.text}
        </span>
        <span className="tw-devtools-log-meta">{formatClock(entry.at)}</span>
      </button>
      {open && (
        <div className="tw-devtools-log-more">
          {entry.stack && <pre className="tw-devtools-stack">{entry.stack}</pre>}
          {page && <p className="tw-devtools-log-page">{page}</p>}
        </div>
      )}
    </li>
  );
};

interface IConsolePanelProps {
  devtools: TPreviewDevtools;
  query: string;
  search: ReactNode;
}

/** Chrome's console, read-only: the page's errors and messages, repeats folded into a count. */
export const ConsolePanel = ({ devtools, query, search }: IConsolePanelProps) => {
  const [filter, setFilter] = useState<TConsoleFilter>(CONSOLE_FILTER.all);
  const entries = devtools.log.console;
  const filtered = useMemo(() => filterConsole(entries, filter, query), [entries, filter, query]);
  const { shown, hidden } = tail(filtered);
  const scrollRef = useStickToBottom<HTMLDivElement>(`${shown.length}:${shown.at(-1)?.id ?? 0}`);

  return (
    <div className="tw-devtools-console">
      <div className="tw-devtools-filters" role="group" aria-label={t('devtools.consolePanel.levels')}>
        {([CONSOLE_FILTER.errors, CONSOLE_FILTER.warnings, CONSOLE_FILTER.all] as const).map((value) => (
          <button
            key={value}
            type="button"
            className="tw-devtools-filter"
            aria-pressed={filter === value}
            onClick={() => setFilter(value)}
          >
            {FILTER_LABELS[value]}
          </button>
        ))}
        {search}
      </div>
      <div ref={scrollRef} className="tw-devtools-scroll">
        {entries.length === 0 && <p className="tw-devtools-empty">{t('devtools.consolePanel.empty')}</p>}
        {entries.length > 0 && filtered.length === 0 && (
          <p className="tw-devtools-empty">{t('devtools.consolePanel.noMatch')}</p>
        )}
        {hidden > 0 && (
          <p className="tw-devtools-capped">{plural(shown.length, 'devtools.consolePanel.capped')}</p>
        )}
        {shown.length > 0 && (
          <ul className="tw-devtools-logs" aria-label={t('devtools.consolePanel.messages')}>
            {shown.map((entry) => (
              <Entry key={entry.id} entry={entry} />
            ))}
          </ul>
        )}
      </div>
    </div>
  );
};
