// «MCP servers» (Settings → Connections, under trackers and wikis): the person's remote MCP servers, whose tools agents
// in new chats get. The list: what is connected, one row each with how it stands; the catalog of known services under
// a search, in groups; one compact form for the address of a server of one's own. A press on a service opens its own
// page (service-page.tsx, the address /settings/connections/<id>) where it is explained, connected and managed: the
// list stays as it was behind it, so «back» finds the same place.
import { useCallback, useState } from 'react';
import {
  MCP_AUTH,
  type IMcpAddInput,
  type IMcpAdded,
  type IMcpAuthStart,
  type IMcpList,
  type IOwnServiceInput,
} from '@/next/features/connections/mcp-api';
import { ConnectForm, type TConnectMcp, type TProbeResult } from '@/next/features/connections/mcp-catalog';
import {
  isKeyed,
  mcpReasonText,
  needsPerson,
  openCatalog,
  takesToken,
} from '@/next/features/connections/mcp-model';
import OwnServiceBlock from '@/next/features/connections/own-service';
import { useMcpSignIn } from '@/next/features/connections/mcp-signin';
import type { IMcpActions, ITokenAsk } from '@/next/features/connections/service-manage';
import { CatalogGroups, CatalogSearch, ServerRow } from '@/next/features/connections/service-list';
import { findService } from '@/next/features/connections/service-model';
import ServicePage from '@/next/features/connections/service-page';
import { Group } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/providers.css';
import '@/next/styles/connections.css';
import '@/next/styles/connections-page.css';

export type { IMcpActions } from '@/next/features/connections/service-manage';
export type TAddResult = IMcpAdded | { error: string };
export type TSignInStart = { authUrl: string } | { error: string };

export interface IMcpSectionProps extends IMcpActions {
  /** null while the list is on its way. */
  mcp: IMcpList | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  /** The service whose page is open (the address's `item`), or null for the list. */
  item?: string | null;
  /** Opens the page of a service by its address; null goes back to the list. */
  onOpen?: (item: string | null) => void;
  onProbe: (url: string) => Promise<TProbeResult>;
  onAdd: (input: IMcpAddInput) => Promise<TAddResult>;
  /** Makes an API or a local server of the person's own from the form (the keys typed go to the safe). */
  onAddOwn?: (input: IOwnServiceInput, values: Readonly<Record<string, string>>) => Promise<TAddResult>;
  onSignIn: (id: string) => Promise<TSignInStart>;
  /** Reads the list again; its answer replaces the page's. */
  onRefresh: () => Promise<void>;
}

/** After POST /mcp: the popup's sign-in, the token form, or nothing when the server connected at once. */
const NEXT = { signIn: 'signIn', token: 'token', none: 'none' } as const;
type TNext = (typeof NEXT)[keyof typeof NEXT];

const nextStep = (added: IMcpAdded): TNext => {
  // A local server or an API has no sign-in: its keys went with the request.
  if (isKeyed(added.server)) return NEXT.none;
  const start: IMcpAuthStart | null = added.authStart;
  if (start === null) {
    // A runtime that does not say: the server's own state decides.
    if (!needsPerson(added.server)) return NEXT.none;
    return takesToken(added.server) ? NEXT.token : NEXT.signIn;
  }
  if (start.auth === MCP_AUTH.header || start.oauthUnavailable) return NEXT.token;
  if (start.auth === MCP_AUTH.oauth) return NEXT.signIn;
  return NEXT.none;
};

const RowsSkeleton = () => (
  <Delayed>
    <div className="hx-conn-sk">
      <Skeleton className="hx-conn-sk__row" />
      <Skeleton className="hx-conn-sk__row" />
    </div>
  </Delayed>
);

const Head = () => (
  <div className="hx-mcp__head">
    <div className="hx-mcp__title">{t('next.connections.mcp.title')}</div>
    <p className="hx-mcp__lead">{t('next.connections.mcp.lead')}</p>
  </div>
);

const noop = () => {};

const McpSection = ({
  mcp,
  failed = null,
  item = null,
  onOpen = noop,
  onProbe,
  onAdd,
  onAddOwn,
  onSignIn,
  onRefresh,
  ...actions
}: IMcpSectionProps) => {
  const servers = mcp === null ? null : mcp.servers;
  const signin = useMcpSignIn({ servers, onStart: onSignIn, onRefresh, reasonText: mcpReasonText });
  const { prepare, release, start } = signin;
  const [ask, setAsk] = useState<ITokenAsk | null>(null);
  const [query, setQuery] = useState('');

  /** Keeps the server, then signs in to it when it asks for that. Called in the click: the popup opens first. */
  const connect: TConnectMcp = useCallback(
    async (input, expectOAuth) => {
      const slot = expectOAuth ? prepare() : { win: null };
      const got = await onAdd(input);
      if ('error' in got) {
        release(slot);
        return got.error;
      }
      const next = nextStep(got);
      if (next === NEXT.signIn) {
        void start(got.server.id, slot);
        return null;
      }
      release(slot);
      if (next === NEXT.token)
        setAsk({
          id: got.server.id,
          headerName: got.authStart === null ? '' : got.authStart.headerName,
          tokenUrl: got.authStart === null ? '' : got.authStart.tokenUrl,
        });
      return null;
    },
    [onAdd, prepare, release, start],
  );

  if (mcp === null)
    return (
      <div className="hx-mcp">
        {item === null && <Head />}
        <Group>
          {failed === null && <RowsSkeleton />}
          {failed !== null && (
            <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
          )}
        </Group>
      </div>
    );

  const service = item === null ? null : findService(item, mcp.servers, mcp.catalog);
  const open = openCatalog(mcp.catalog, mcp.servers);
  return (
    <div className="hx-mcp">
      {item !== null && service !== null && (
        <ServicePage
          key={item}
          service={service}
          actions={actions}
          signin={signin}
          onConnect={connect}
          ask={ask}
          onBack={() => onOpen(null)}
        />
      )}
      {item !== null && service === null && (
        <div className="hx-svc">
          <button type="button" className="hx-svc__back" onClick={() => onOpen(null)}>
            <Icon name="chevron-left" size="sm" />
            {t('next.connections.svc.back')}
          </button>
          <p className="hx-mcp-none">{t('next.connections.svc.unknown')}</p>
        </div>
      )}
      <div className="hx-mcp__list" hidden={item !== null}>
        <Head />
        {mcp.servers.length > 0 && (
          <section className="hx-cn-block">
            <h3 className="hx-cn-block__title">{t('next.connections.svc.connected')}</h3>
            <div className="hx-cn-list">
              {mcp.servers.map((server) => (
                <ServerRow key={server.id} server={server} onOpen={onOpen} />
              ))}
            </div>
          </section>
        )}
        <section className="hx-cn-block">
          <h3 className="hx-cn-block__title">{t('next.connections.svc.add')}</h3>
          {open.length > 0 && <CatalogSearch query={query} onQuery={setQuery} />}
          <CatalogGroups catalog={open} query={query} onOpen={onOpen} />
        </section>
        <OwnServiceBlock
          onAdd={
            onAddOwn ??
            (async () => ({ error: t('next.connections.mcp.reason.other', { code: 'unavailable' }) }))
          }
          onOpen={onOpen}
          mcpForm={<ConnectForm onProbe={onProbe} onConnect={connect} />}
        />
        <p className="hx-set__foot hx-mcp__foot">{t('next.connections.mcp.foot')}</p>
      </div>
    </div>
  );
};

export default McpSection;
