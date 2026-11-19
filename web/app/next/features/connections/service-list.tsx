// The lists of Settings → Connections → MCP servers: what is connected (a row each, with its state) and the catalog of
// services in groups under a search. A press on any row opens the service's own page; nothing is filled in or kept
// here.
import { useId, useState, type ReactNode } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import type { IMcpCatalogEntry, IMcpServer, TMcpKind } from '@/next/features/connections/mcp-api';
import { blurbOf, catalogGroups, mcpSubLine, toolsText } from '@/next/features/connections/mcp-model';
import { itemOf, statusOf } from '@/next/features/connections/service-model';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

interface IServerRowProps {
  server: IMcpServer;
  onOpen: (item: string) => void;
}

/** A connected server: its name, whose it is, how it stands, and the way to its page. */
export const ServerRow = ({ server, onOpen }: IServerRowProps) => {
  const status = statusOf(server);
  return (
    <button
      type="button"
      className="hx-cn-row"
      data-off={!server.enabled || undefined}
      onClick={() => onOpen(itemOf(server))}
    >
      <McpGlyph id={server.catalogId} name={server.name} letter={server.custom?.letter ?? ''} />
      <span className="hx-cn-row__text">
        <span className="hx-cn-row__name">{server.name}</span>
        <span className="hx-cn-row__sub">
          {mcpSubLine(server)}
          {server.tools.length > 0 && ` · ${toolsText(server.tools.length)}`}
        </span>
      </span>
      <span className="hx-svc-pill" data-tone={status.tone}>
        <span className="hx-svc-pill__dot" aria-hidden="true" />
        {status.label}
      </span>
      <Icon name="chevron-right" size="sm" className="hx-cn-row__go" />
    </button>
  );
};

interface ICatalogRowProps {
  entry: IMcpCatalogEntry;
  onOpen: (item: string) => void;
}

/** A service of the catalog: its logo, name and what it gives in a line or two. */
const CatalogRow = ({ entry, onOpen }: ICatalogRowProps) => (
  <button type="button" className="hx-cn-row" onClick={() => onOpen(entry.id)}>
    <McpGlyph id={entry.id} name={entry.name} />
    <span className="hx-cn-row__text">
      <span className="hx-cn-row__name">{entry.name}</span>
      {blurbOf(entry) !== '' && <span className="hx-cn-row__blurb">{blurbOf(entry)}</span>}
    </span>
    <Icon name="chevron-right" size="sm" className="hx-cn-row__go" />
  </button>
);

interface ICatalogSearchProps {
  query: string;
  onQuery: (query: string) => void;
}

/** A search over the catalog's names, what each gives and its group; it filters as the person types. */
export const CatalogSearch = ({ query, onQuery }: ICatalogSearchProps) => (
  <div className="hx-cn-search">
    <Icon name="search" size="sm" className="hx-cn-search__icon" />
    <input
      className="hx-cn-search__input"
      type="search"
      value={query}
      autoComplete="off"
      spellCheck={false}
      aria-label={t('next.connections.mcp.search')}
      placeholder={t('next.connections.mcp.search')}
      onChange={(event) => onQuery(event.target.value)}
    />
  </div>
);

interface IGroupProps {
  kind: TMcpKind;
  items: readonly IMcpCatalogEntry[];
  /** Open: the search found something in it. */
  forced: boolean;
  onOpen: (item: string) => void;
}

/** One group of the catalog, folded to its name and count until the person (or a search) opens it. */
const CatalogGroup = ({ kind, items, forced, onOpen }: IGroupProps) => {
  const [open, setOpen] = useState(false);
  const id = useId();
  const shown = open || forced;
  return (
    <section className="hx-cn-group" data-open={shown}>
      <h3 className="hx-cn-group__title">
        <button
          type="button"
          className="hx-cn-group__toggle"
          aria-expanded={shown}
          aria-controls={id}
          onClick={() => setOpen(!shown)}
        >
          <span className="hx-cn-group__name">{t(`next.connections.mcp.kind.${kind}`)}</span>
          <span className="hx-cn-group__count">{items.length}</span>
          <Icon name="chevron-down" size="sm" className="hx-cn-group__chev" />
        </button>
      </h3>
      {shown && (
        <div id={id} className="hx-cn-list">
          {items.map((entry) => (
            <CatalogRow key={entry.id} entry={entry} onOpen={onOpen} />
          ))}
        </div>
      )}
    </section>
  );
};

interface ICatalogGroupsProps {
  catalog: readonly IMcpCatalogEntry[];
  query: string;
  onOpen: (item: string) => void;
}

/** The catalog the query finds, in its groups: all folded without a query, the matching ones open with it. */
export const CatalogGroups = ({ catalog, query, onOpen }: ICatalogGroupsProps) => {
  const groups = catalogGroups(catalog, (entry) => entry, query);
  const searching = query.trim() !== '';
  return (
    <div className="hx-cn-groups">
      {groups.map((group) => (
        <CatalogGroup
          key={group.kind}
          kind={group.kind}
          items={group.items}
          forced={searching}
          onOpen={onOpen}
        />
      ))}
      {groups.length === 0 && <p className="hx-mcp-none">{t('next.connections.mcp.nothingFound')}</p>}
    </div>
  );
};

/** The address of a server that is in no catalog: folded like a group, the form (ConnectForm) opens in it. */
export const CustomServer = ({ children, title }: { children: ReactNode; title?: string }) => {
  const [open, setOpen] = useState(false);
  const id = useId();
  return (
    <section className="hx-cn-group" data-open={open}>
      <h3 className="hx-cn-group__title">
        <button
          type="button"
          className="hx-cn-group__toggle"
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setOpen(!open)}
        >
          <Icon name="plus" size="sm" className="hx-cn-group__plus" />
          <span className="hx-cn-group__name">{title ?? t('next.connections.svc.own')}</span>
          <Icon name="chevron-down" size="sm" className="hx-cn-group__chev" />
        </button>
      </h3>
      {open && (
        <div id={id} className="hx-cn-own">
          {children}
        </div>
      )}
    </section>
  );
};
