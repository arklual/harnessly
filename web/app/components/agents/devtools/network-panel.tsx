import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react';
import { MoreHorizontal } from 'lucide-react';
import {
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwMenuButton,
  cx,
  useTwContextMenu,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import { blockedBy } from '@/lib/hl-preview/devtools/store';
import type { INetworkEntry } from '@/lib/hl-preview/devtools/types';
import {
  NETWORK_FILTER,
  NETWORK_FILTER_LABELS,
  TYPE_LABELS,
  addressPattern,
  domainPattern,
  filterNetwork,
  formatBytes,
  formatClock,
  formatDuration,
  groupByDomain,
  isFailed,
  isPending,
  networkTotals,
  pluralRequests,
  requestName,
  statusLabel,
  tail,
  timeLabel,
  waterfallBar,
  waterfallWindow,
  withPattern,
  withoutPattern,
  type IWaterfall,
  type TNetworkFilter,
} from '@/lib/hl-preview-devtools-view';
import type { TPreviewDevtools } from '@/hooks/use-preview-devtools';
import { useStickToBottom } from '@/components/agents/devtools/use-stick-to-bottom';
import { t } from '@/lib/i18n';

const rowState = (entry: INetworkEntry): string | undefined => {
  if (entry.blocked) return 'blocked';
  if (isFailed(entry)) return 'failed';
  return isPending(entry) ? 'pending' : undefined;
};

/** A pending request comes back finished under a new id: the start, method and address stay. */
const requestKey = (entry: INetworkEntry) => `${entry.at}\n${entry.method}\n${entry.url}`;

const copy = (text: string) => {
  void navigator.clipboard.writeText(text).catch(() => undefined);
};

/** The bar's place in the window goes through CSS variables of its own element: no inline style attribute. */
const WaterfallBar = ({ entry, scale, now }: { entry: INetworkEntry; scale: IWaterfall; now: number }) => {
  const ref = useRef<HTMLSpanElement>(null);
  const { left, width } = waterfallBar(entry, scale, now);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--tw-devtools-bar-left', `${left}%`);
    ref.current?.style.setProperty('--tw-devtools-bar-width', `${width}%`);
  }, [left, width]);
  return (
    <span className="tw-devtools-cell tw-devtools-cell-waterfall" aria-hidden>
      <span ref={ref} className="tw-devtools-bar" />
    </span>
  );
};

const SOURCE_LABELS = {
  proxy: t('devtools.networkPanel.sourceProxy'),
  page: t('devtools.networkPanel.sourcePage'),
} as const;

const Details = ({ entry, pattern }: { entry: INetworkEntry; pattern: string | null }) => (
  <dl className="tw-devtools-details">
    <dt>{t('devtools.networkPanel.address')}</dt>
    <dd className="tw-devtools-mono tw-devtools-details-url">{entry.url}</dd>
    <dt>{t('devtools.networkPanel.method')}</dt>
    <dd>{entry.method}</dd>
    <dt>{t('devtools.networkPanel.status')}</dt>
    <dd>
      {entry.status ??
        (isPending(entry) ? t('devtools.networkPanel.waiting') : t('devtools.networkPanel.noAnswer'))}
    </dd>
    <dt>{t('devtools.networkPanel.type')}</dt>
    <dd>
      {TYPE_LABELS[entry.type]}
      {entry.mime ? ` · ${entry.mime}` : ''}
    </dd>
    <dt>{t('devtools.networkPanel.size')}</dt>
    <dd>{formatBytes(entry.size)}</dd>
    <dt>{t('devtools.networkPanel.time')}</dt>
    <dd>
      {timeLabel(entry)} · {t('devtools.networkPanel.startedAt', { time: formatClock(entry.at) })}
      {entry.throttled ? ` · ${t('devtools.networkPanel.throttled')}` : ''}
    </dd>
    <dt>{t('devtools.networkPanel.seenBy')}</dt>
    <dd>
      {SOURCE_LABELS[entry.source]}
      {entry.thirdParty ? ` · ${t('devtools.networkPanel.thirdPartySite')}` : ''}
    </dd>
    {entry.blocked && (
      <>
        <dt>{t('devtools.networkPanel.block')}</dt>
        <dd>
          {pattern ? t('devtools.networkPanel.rule', { pattern }) : t('devtools.networkPanel.ruleRemoved')}
        </dd>
      </>
    )}
    {entry.error && (
      <>
        <dt>{t('devtools.networkPanel.error')}</dt>
        <dd className="tw-devtools-danger">{entry.error}</dd>
      </>
    )}
  </dl>
);

interface IRowProps {
  entry: INetworkEntry;
  expanded: boolean;
  scale: IWaterfall;
  now: number;
  pattern: string | null;
  menu: TTwMenuEntry[];
  onToggle: () => void;
}

const Row = ({ entry, expanded, scale, now, pattern, menu, onToggle }: IRowProps) => {
  const name = requestName(entry);
  const tip =
    entry.blocked && pattern
      ? `${t('devtools.networkPanel.blockedBy', { pattern })}\n${entry.url}`
      : entry.url;
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) return;
    event.preventDefault();
    onToggle();
  };
  return (
    <div className="tw-devtools-entry" data-entry-id={entry.id}>
      <div
        role="row"
        tabIndex={0}
        aria-expanded={expanded}
        className="tw-devtools-row"
        data-state={rowState(entry)}
        onClick={onToggle}
        onKeyDown={onKeyDown}
      >
        <span role="cell" className="tw-devtools-cell tw-devtools-cell-name" title={tip}>
          <span className="tw-devtools-name">{name}</span>
          {entry.thirdParty && name !== entry.host && <span className="tw-devtools-host">{entry.host}</span>}
        </span>
        <span
          role="cell"
          className="tw-devtools-cell tw-devtools-cell-status"
          title={entry.error ?? undefined}
        >
          {statusLabel(entry)}
        </span>
        <span role="cell" className="tw-devtools-cell tw-devtools-cell-type">
          {TYPE_LABELS[entry.type]}
        </span>
        <span role="cell" className="tw-devtools-cell tw-devtools-cell-size">
          {formatBytes(entry.size)}
        </span>
        <span role="cell" className="tw-devtools-cell tw-devtools-cell-time">
          {timeLabel(entry)}
        </span>
        <WaterfallBar entry={entry} scale={scale} now={now} />
        <span className="tw-devtools-row-actions">
          <TwMenuButton
            items={menu}
            menuLabel={t('devtools.networkPanel.requestActions')}
            align={LAYER_ALIGN.end}
            className="tw-icon-button tw-devtools-row-more"
            aria-label={t('devtools.networkPanel.requestActions')}
            title={t('devtools.networkPanel.actions')}
          >
            <MoreHorizontal />
          </TwMenuButton>
        </span>
      </div>
      {expanded && <Details entry={entry} pattern={pattern} />}
    </div>
  );
};

interface INetworkPanelProps {
  devtools: TPreviewDevtools;
  query: string;
}

/** Chrome's Network panel: the type filter bar, the request table (or the domains), the totals. */
export const NetworkPanel = ({ devtools, query }: INetworkPanelProps) => {
  const { log, polledAt, update, saving } = devtools;
  const { blocked } = log.settings;
  const [kind, setKind] = useState<TNetworkFilter>(NETWORK_FILTER.all);
  const [byDomain, setByDomain] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [menuEntry, setMenuEntry] = useState<INetworkEntry | null>(null);

  const filtered = useMemo(() => filterNetwork(log.network, kind, query), [log.network, kind, query]);
  const { shown, hidden } = tail(filtered);
  const scale = useMemo(() => waterfallWindow(shown, polledAt), [shown, polledAt]);
  const totals = networkTotals(filtered);
  const domains = useMemo(() => (byDomain ? groupByDomain(filtered) : []), [byDomain, filtered]);
  const scrollRef = useStickToBottom<HTMLDivElement>(
    byDomain ? domains.length : `${shown.length}:${shown.at(-1)?.id ?? 0}`,
  );

  const block = (pattern: string) => void update('blocked', { blocked: withPattern(blocked, pattern) });
  const unblock = (pattern: string) => void update('blocked', { blocked: withoutPattern(blocked, pattern) });
  const entryMenu = (entry: INetworkEntry): TTwMenuEntry[] => {
    const address = addressPattern(entry.url);
    const domain = domainPattern(entry.host);
    const busy = saving !== null;
    return [
      blocked.includes(address)
        ? {
            kind: TW_MENU_KIND.action,
            id: 'unblock-url',
            label: t('devtools.networkPanel.unblockAddress'),
            disabled: busy,
            onSelect: () => unblock(address),
          }
        : {
            kind: TW_MENU_KIND.action,
            id: 'block-url',
            label: t('devtools.networkPanel.blockAddress'),
            hint: address,
            disabled: busy,
            onSelect: () => block(address),
          },
      blocked.includes(domain)
        ? {
            kind: TW_MENU_KIND.action,
            id: 'unblock-host',
            label: t('devtools.networkPanel.unblockDomain'),
            disabled: busy,
            onSelect: () => unblock(domain),
          }
        : {
            kind: TW_MENU_KIND.action,
            id: 'block-host',
            label: t('devtools.networkPanel.blockDomain'),
            hint: entry.host,
            disabled: busy,
            onSelect: () => block(domain),
          },
      { kind: TW_MENU_KIND.separator, id: 'copy-separator' },
      {
        kind: TW_MENU_KIND.action,
        id: 'copy',
        label: t('devtools.networkPanel.copyAddress'),
        onSelect: () => copy(entry.url),
      },
    ];
  };
  const context = useTwContextMenu({
    items: menuEntry ? entryMenu(menuEntry) : [],
    'aria-label': t('devtools.networkPanel.requestActions'),
  });
  const pickMenuEntry = (event: MouseEvent<HTMLElement>) => {
    const id = Number(
      (event.target as HTMLElement).closest('[data-entry-id]')?.getAttribute('data-entry-id'),
    );
    setMenuEntry(shown.find((entry) => entry.id === id) ?? null);
  };

  const filters = Object.values(NETWORK_FILTER);
  const empty = log.network.length === 0;

  return (
    <div className="tw-devtools-network">
      <div className="tw-devtools-filters" role="group" aria-label={t('devtools.networkPanel.requestType')}>
        <button
          type="button"
          className="tw-devtools-filter"
          aria-pressed={byDomain}
          onClick={() => setByDomain(!byDomain)}
        >
          {t('devtools.networkPanel.byDomain')}
        </button>
        <span className="tw-devtools-filters-divider" aria-hidden />
        {filters.map((filter) => (
          <button
            key={filter}
            type="button"
            className="tw-devtools-filter"
            aria-pressed={kind === filter}
            onClick={() => setKind(filter)}
          >
            {NETWORK_FILTER_LABELS[filter]}
          </button>
        ))}
      </div>
      <div ref={scrollRef} className="tw-devtools-scroll">
        {empty && <p className="tw-devtools-empty">{t('devtools.networkPanel.empty')}</p>}
        {!empty && filtered.length === 0 && (
          <p className="tw-devtools-empty">{t('devtools.networkPanel.noMatch')}</p>
        )}
        {!empty && filtered.length > 0 && byDomain && (
          <div
            role="table"
            aria-label={t('devtools.networkPanel.domains')}
            className="tw-devtools-table tw-devtools-domains"
          >
            <div role="row" className="tw-devtools-row tw-devtools-head-row">
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-name">
                {t('devtools.networkPanel.domain')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-count">
                {t('devtools.networkPanel.requests')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-size">
                {t('devtools.networkPanel.size')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-count">
                {t('devtools.networkPanel.errors')}
              </span>
            </div>
            {domains.map((row) => {
              const pattern = domainPattern(row.host);
              const isBlocked = blocked.includes(pattern);
              const menu: TTwMenuEntry[] = [
                isBlocked
                  ? {
                      kind: TW_MENU_KIND.action,
                      id: 'unblock',
                      label: t('devtools.networkPanel.unblockDomain'),
                      disabled: saving !== null,
                      onSelect: () => unblock(pattern),
                    }
                  : {
                      kind: TW_MENU_KIND.action,
                      id: 'block',
                      label: t('devtools.networkPanel.blockDomain'),
                      disabled: saving !== null,
                      onSelect: () => block(pattern),
                    },
                {
                  kind: TW_MENU_KIND.action,
                  id: 'copy',
                  label: t('devtools.networkPanel.copyDomain'),
                  onSelect: () => copy(row.host),
                },
              ];
              return (
                <div
                  key={row.host}
                  role="row"
                  className="tw-devtools-row"
                  data-state={isBlocked ? 'blocked' : undefined}
                >
                  <span role="cell" className="tw-devtools-cell tw-devtools-cell-name" title={row.host}>
                    <span className="tw-devtools-name">{row.host}</span>
                    {row.thirdParty && (
                      <span className="tw-devtools-host">{t('devtools.networkPanel.thirdParty')}</span>
                    )}
                  </span>
                  <span role="cell" className="tw-devtools-cell tw-devtools-cell-count">
                    {row.requests}
                  </span>
                  <span role="cell" className="tw-devtools-cell tw-devtools-cell-size">
                    {formatBytes(row.bytes)}
                  </span>
                  <span
                    role="cell"
                    className={cx(
                      'tw-devtools-cell tw-devtools-cell-count',
                      row.failed > 0 && 'tw-devtools-danger',
                    )}
                  >
                    {row.failed > 0 ? row.failed : '—'}
                  </span>
                  <span className="tw-devtools-row-actions">
                    <TwMenuButton
                      items={menu}
                      menuLabel={t('devtools.networkPanel.domainActions')}
                      align={LAYER_ALIGN.end}
                      className="tw-icon-button tw-devtools-row-more"
                      aria-label={t('devtools.networkPanel.domainActionsFor', { host: row.host })}
                      title={t('devtools.networkPanel.actions')}
                    >
                      <MoreHorizontal />
                    </TwMenuButton>
                  </span>
                </div>
              );
            })}
          </div>
        )}
        {!empty && filtered.length > 0 && !byDomain && (
          <div
            role="table"
            aria-label={t('devtools.networkPanel.pageRequests')}
            className="tw-devtools-table tw-devtools-requests"
          >
            <div role="row" className="tw-devtools-row tw-devtools-head-row">
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-name">
                {t('devtools.networkPanel.name')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-status">
                {t('devtools.networkPanel.status')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-type">
                {t('devtools.networkPanel.type')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-size">
                {t('devtools.networkPanel.size')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-time">
                {t('devtools.networkPanel.time')}
              </span>
              <span role="columnheader" className="tw-devtools-cell tw-devtools-cell-waterfall">
                {t('devtools.networkPanel.waterfall')}
              </span>
            </div>
            {hidden > 0 && (
              <p className="tw-devtools-capped">
                {t('devtools.networkPanel.capped', { shown: shown.length, hidden: pluralRequests(hidden) })}
              </p>
            )}
            <div role="rowgroup" onContextMenuCapture={pickMenuEntry} {...context.bind}>
              {shown.map((entry) => (
                <Row
                  key={entry.id}
                  entry={entry}
                  expanded={expanded === requestKey(entry)}
                  scale={scale}
                  now={polledAt}
                  pattern={entry.blocked ? blockedBy(entry.url, blocked) : null}
                  menu={entryMenu(entry)}
                  onToggle={() => setExpanded(expanded === requestKey(entry) ? null : requestKey(entry))}
                />
              ))}
            </div>
            {context.menu}
          </div>
        )}
      </div>
      {!empty && (
        <footer className="tw-devtools-footer">
          {filtered.length === log.network.length
            ? pluralRequests(totals.requests)
            : t('devtools.networkPanel.filteredOf', {
                shown: filtered.length,
                total: pluralRequests(log.network.length),
              })}
          {' · '}
          {t('devtools.networkPanel.transferred', { size: formatBytes(totals.bytes) })}
          {totals.spanMs > 0 && ` · ${formatDuration(totals.spanMs)}`}
        </footer>
      )}
    </div>
  );
};
