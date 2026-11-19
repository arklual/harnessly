// The page of one service (Settings → Connections → a service, /settings/connections/<id>): who it is, what an agent
// can do with it, how to connect it step by step with the fields right there, links worth keeping, and for a connected
// server its state with the buttons to manage it and its tools. Nothing is kept until the person presses the button of
// the step; connecting is the same call the catalog's form always made.
import { useRef, useState, type ReactNode } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  MCP_TRANSPORT,
  type IMcpCatalogEntry,
  type IMcpServer,
} from '@/next/features/connections/mcp-api';
import {
  EnvFields,
  headerOf,
  StartingHint,
  useEnvValues,
  type TConnectMcp,
} from '@/next/features/connections/mcp-catalog';
import { envPayload, envReady, isKeyed, toolsText } from '@/next/features/connections/mcp-model';
import type { ISignIn } from '@/next/features/connections/mcp-signin';
import ServerManage, { type IMcpActions, type ITokenAsk } from '@/next/features/connections/service-manage';
import {
  aboutParts,
  canFromTools,
  canOf,
  linkHost,
  linkName,
  linksOf,
  stepsOf,
  statusOf,
  type IService,
} from '@/next/features/connections/service-model';
import { hostOf } from '@/next/features/connections/model';
import OwnDescribe from '@/next/features/connections/own-describe';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import SecretField from '@/next/ui/secret-field';
import '@/next/styles/connections-page.css';

const Section = ({ title, children, id }: { title: string; children: ReactNode; id: string }) => (
  <section className="hx-svc-sec" aria-labelledby={id}>
    <h3 className="hx-svc-sec__title" id={id}>
      {title}
    </h3>
    {children}
  </section>
);

/** «Create a token» and its like: a link that looks like a button, to a page of the service. */
const OutLink = ({ href, children }: { href: string; children: ReactNode }) => (
  <a className="hx-svc-out" href={href} target="_blank" rel="noreferrer">
    {children}
    <Icon name="external-link" size="sm" />
  </a>
);

interface ISteps {
  steps: readonly string[];
  /** The step the fields belong to: they sit under the last one. */
  children: ReactNode;
}

/** Numbered steps, and under them the fields and the button that do the last of them. */
const Steps = ({ steps, children }: ISteps) => (
  <div className="hx-svc-card">
    <ol className="hx-svc-steps">
      {steps.map((step, at) => (
        <li key={step} className="hx-svc-steps__item">
          <span className="hx-svc-steps__n" aria-hidden="true">
            {at + 1}
          </span>
          <span className="hx-svc-steps__text">{step}</span>
        </li>
      ))}
    </ol>
    <div className="hx-svc-form">{children}</div>
  </div>
);

interface IConnectProps {
  entry: IMcpCatalogEntry;
  onConnect: TConnectMcp;
}

/** The fields and the one button that connect a catalog service: sign in on its page, a token, keys, or nothing. */
const ConnectFields = ({ entry, onConnect }: IConnectProps) => {
  const keyed = isKeyed(entry);
  const env = useEnvValues(entry.env, true);
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);
  const oauth = !keyed && entry.auth === MCP_AUTH.oauth;
  const takesToken = !keyed && entry.auth === MCP_AUTH.header;
  const ready = keyed ? envReady(entry.env, env.values) : !takesToken || token.trim() !== '';

  const connect = async () => {
    if (busy || !ready) return;
    setBusy(true);
    setError(null);
    // Called in the click, before any await: the sign-in popup opens in it.
    const pending = onConnect(
      {
        url: keyed ? '' : entry.url,
        name: '',
        catalogId: entry.id,
        transport: keyed ? null : entry.transport,
        header: takesToken ? headerOf(entry.headerName || DEFAULT_HEADER, token, entry.tokenPrefix) : null,
        ...(keyed ? { env: envPayload(entry.env, env.values) } : {}),
      },
      oauth,
    );
    setToken('');
    const refused = await pending;
    if (!alive.current) return;
    setBusy(false);
    setError(refused);
  };

  const local = entry.transport === MCP_TRANSPORT.stdio;
  return (
    <>
      {keyed && entry.env.length > 0 && (
        <EnvFields
          fields={entry.env}
          values={env.values}
          keep={false}
          disabled={busy}
          invalid={error !== null}
          onChange={(name, value) => {
            env.set(name, value);
            setError(null);
          }}
          onSubmit={() => void connect()}
        />
      )}
      {takesToken && (
        <div className="hx-svc-token">
          {entry.tokenUrl !== '' && (
            <OutLink href={entry.tokenUrl}>{t('next.connections.createToken')}</OutLink>
          )}
          <div className="hx-conn-form__field">
            <span className="hx-conn-form__label">{t('next.connections.mcp.header.value')}</span>
            <SecretField
              value={token}
              onChange={(next) => {
                setToken(next);
                setError(null);
              }}
              label={t('next.connections.mcp.header.value')}
              placeholder={t('next.connections.svc.tokenPlaceholder')}
              disabled={busy}
              invalid={error !== null}
              onSubmit={() => void connect()}
            />
          </div>
        </div>
      )}
      {keyed && !local && <p className="hx-svc-note">{t('next.connections.mcp.apiNote')}</p>}
      {keyed && local && <p className="hx-svc-note">{t('next.connections.mcp.localNote')}</p>}
      {busy && keyed && <StartingHint />}
      <div className="hx-svc-form__go">
        <SetButton
          tone="primary"
          icon={oauth ? 'log-in' : undefined}
          busy={busy}
          disabled={!ready}
          onClick={() => void connect()}
        >
          {oauth ? t('next.connections.mcp.signInWith', { name: entry.name }) : t('next.connections.connect')}
        </SetButton>
      </div>
      {error !== null && <FormError>{error}</FormError>}
    </>
  );
};

/** The bullets of what an agent can do. */
const CanList = ({ lines }: { lines: readonly string[] }) => (
  <ul className="hx-svc-can">
    {lines.map((line) => (
      <li key={line} className="hx-svc-can__item">
        <Icon name="check" size="sm" className="hx-svc-can__tick" />
        <span>{line}</span>
      </li>
    ))}
  </ul>
);

/** The server's tools, folded: a name and what each does. */
const ToolsFold = ({ server }: { server: IMcpServer }) => (
  <details className="hx-svc-tools">
    <summary className="hx-svc-tools__sum">
      <span>{toolsText(server.tools.length)}</span>
      <Icon name="chevron-down" size="sm" className="hx-svc-tools__chev" />
    </summary>
    <ul className="hx-mcp-tools__list" aria-label={t('next.connections.mcp.toolsOf', { name: server.name })}>
      {server.tools.map((tool) => (
        <li key={tool.name} className="hx-mcp-tools__item">
          <span className="hx-mcp-tools__name">{tool.name}</span>
          {tool.description !== '' && <span className="hx-mcp-tools__desc">{tool.description}</span>}
        </li>
      ))}
    </ul>
  </details>
);

const Links = ({ entry }: { entry: IMcpCatalogEntry }) => {
  const links = linksOf(entry);
  if (links.length === 0) return null;
  return (
    <ul className="hx-svc-links">
      {links.map((link) => (
        <li key={link.url}>
          <a className="hx-svc-link" href={link.url} target="_blank" rel="noreferrer">
            <span className="hx-svc-link__text">
              <span className="hx-svc-link__name">{linkName(link.kind)}</span>
              <span className="hx-svc-link__host">{linkHost(link.url)}</span>
            </span>
            <Icon name="external-link" size="sm" className="hx-svc-link__go" />
          </a>
        </li>
      ))}
    </ul>
  );
};

export interface IServicePageProps {
  service: IService;
  actions: IMcpActions;
  signin: ISignIn;
  onConnect: TConnectMcp;
  /** The token the server asked for when it was kept, when it did. */
  ask: ITokenAsk | null;
  onBack: () => void;
}

const ServicePage = ({ service, actions, signin, onConnect, ask, onBack }: IServicePageProps) => {
  const { entry, server } = service;
  const name = server === null ? (entry?.name ?? '') : server.name;
  const catalogId = entry === null ? '' : entry.id;
  const { line, rest: longAbout } = entry === null ? { line: '', rest: '' } : aboutParts(entry);
  const real = server !== null && server.tools.length > 0 ? canFromTools(server.tools) : [];
  const can = real.length >= 3 ? real : entry === null ? [] : canOf(entry);
  const tone = server === null ? undefined : statusOf(server).tone;
  const where = server === null ? (entry?.url ?? '') : server.url;

  return (
    <article className="hx-svc" data-state={tone}>
      <button type="button" className="hx-svc__back" onClick={onBack}>
        <Icon name="chevron-left" size="sm" />
        {t('next.connections.svc.back')}
      </button>
      <header className="hx-svc__head">
        <McpGlyph id={catalogId} name={name} letter={server?.custom?.letter ?? ''} size="lg" />
        <div className="hx-svc__titles">
          <h2 className="hx-svc__name">{name}</h2>
          {line !== '' && <p className="hx-svc__line">{line}</p>}
          {where !== '' && !where.includes('{') && server === null && (
            <p className="hx-svc__host">{hostOf(where)}</p>
          )}
        </div>
      </header>
      {longAbout !== '' && <p className="hx-svc__about">{longAbout}</p>}

      {server !== null && (
        <ServerManage
          key={server.id}
          server={server}
          tokenUrl={entry === null ? '' : entry.tokenUrl}
          tokenPrefix={entry === null ? '' : entry.tokenPrefix}
          ask={ask !== null && ask.id === server.id ? ask : null}
          actions={actions}
          signin={signin}
          onGone={onBack}
        />
      )}

      {server !== null && server.custom !== null && actions.onDescribe !== undefined && (
        <OwnDescribe key={`${server.id}-${server.name}`} server={server} onSave={actions.onDescribe} />
      )}

      {can.length > 0 && (
        <Section id="hx-svc-can" title={t('next.connections.svc.can')}>
          <CanList lines={can} />
          {real.length >= 3 && server !== null && (
            <p className="hx-svc-note">{t('next.connections.svc.canReal', { n: server.tools.length })}</p>
          )}
        </Section>
      )}

      {server === null && entry !== null && (
        <Section id="hx-svc-how" title={t('next.connections.svc.how')}>
          <Steps steps={stepsOf(entry)}>
            <ConnectFields entry={entry} onConnect={onConnect} />
          </Steps>
        </Section>
      )}

      {server !== null && server.tools.length > 0 && <ToolsFold server={server} />}

      {entry !== null && linksOf(entry).length > 0 && (
        <Section id="hx-svc-links" title={t('next.connections.svc.links')}>
          <Links entry={entry} />
        </Section>
      )}
    </article>
  );
};

export default ServicePage;
