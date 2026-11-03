// «MCP servers» (Settings → Connections, under trackers and wikis): the person's remote MCP servers, whose tools agents
// in new chats get. A connected server is one row — its name above everything else, then its address and whose account
// it uses; it speaks of its state only when it needs the person (sign in again, a new token) or did not answer. Its
// tools fold out under it. Its actions are in a menu: check again, rename, turn off for agents, disconnect. Under the
// list, one form for a server's address, and the catalog of known services that fills it.
import { Menu } from '@base-ui/react/menu';
import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { McpGlyph } from '@/next/features/connections/glyphs';
import {
  DEFAULT_HEADER,
  MCP_AUTH,
  type IMcpAddInput,
  type IMcpAdded,
  type IMcpAuthStart,
  type IMcpHeader,
  type IMcpList,
  type IMcpServer,
} from '@/next/features/connections/mcp-api';
import {
  CatalogCard,
  ConnectForm,
  HeaderFields,
  headerOf,
  type IPicked,
  type TConnectMcp,
  type TProbeResult,
} from '@/next/features/connections/mcp-catalog';
import {
  isQuietState,
  mcpReasonText,
  mcpStateText,
  mcpSubLine,
  needsPerson,
  openCatalog,
  takesToken,
  toolsText,
} from '@/next/features/connections/mcp-model';
import { FLOW, POPUP_NAME, useMcpSignIn, type ISignIn } from '@/next/features/connections/mcp-signin';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { useLeavingList, useSeen } from '@/next/ui/presence';
import { Delayed, Skeleton } from '@/next/ui/skeleton';
import '@/next/styles/providers.css';
import '@/next/styles/connections.css';

/** A removed row folds for as long as .hx-reveal's transition. */
const LEAVE_MS = 280;
/** «Works» after «Check again» stays this long. */
const CHECKED_MS = 2500;
/** The catalog shows this many services until «Show all». */
const CATALOG_FIRST = 6;
/** The lists' stand-ins while they are on their way: one array each, so the leaving lists see no change. */
const NO_SERVERS: readonly IMcpServer[] = [];

const PANEL = { rename: 'rename', remove: 'remove', token: 'token' } as const;
type TPanel = (typeof PANEL)[keyof typeof PANEL];

const ACTION = {
  check: 'check',
  rename: 'rename',
  toggle: 'toggle',
  remove: 'remove',
  token: 'token',
} as const;
type TAction = (typeof ACTION)[keyof typeof ACTION];

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export type TAddResult = IMcpAdded | { error: string };
export type TSignInStart = { authUrl: string } | { error: string };

export interface IMcpActions {
  onCheck: (id: string) => TDone;
  onRename: (id: string, name: string) => TDone;
  onEnable: (id: string, enabled: boolean) => TDone;
  onRemove: (id: string) => TDone;
  onHeader: (id: string, header: IMcpHeader) => TDone;
}

export interface IMcpSectionProps extends IMcpActions {
  /** null while the list is on its way. */
  mcp: IMcpList | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  onProbe: (url: string) => Promise<TProbeResult>;
  onAdd: (input: IMcpAddInput) => Promise<TAddResult>;
  onSignIn: (id: string) => Promise<TSignInStart>;
  /** Reads the list again; its answer replaces the page's. */
  onRefresh: () => Promise<void>;
}

const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

interface IRowMenuProps {
  server: IMcpServer;
  disabled: boolean;
  onAction: (action: TAction) => void;
}

const RowMenu = ({ server, disabled, onAction }: IRowMenuProps) => {
  const actions = useMenuActions();
  const label = t('next.connections.menu', { name: server.name });
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        disabled={disabled}
        render={
          <button type="button" className="hx-conn-row__more" aria-label={label}>
            <Icon name="ellipsis" size="sm" />
          </button>
        }
      />
      <MenuPanel actions={actions} className="hx-cmenu" label={label} align="end" sideOffset={6}>
        <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.check)}>
          <Icon name="refresh-cw" size="sm" />
          <span className="hx-cmenu__txt">{t('next.connections.check')}</span>
        </Menu.Item>
        <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.rename)}>
          <Icon name="pencil" size="sm" />
          <span className="hx-cmenu__txt">{t('next.connections.rename')}</span>
        </Menu.Item>
        <Menu.Item className="hx-cmenu__item" onClick={() => onAction(ACTION.toggle)}>
          <Icon name={server.enabled ? 'circle-stop' : 'play'} size="sm" />
          <span className="hx-cmenu__txt">
            {server.enabled ? t('next.connections.mcp.turnOff') : t('next.connections.mcp.turnOn')}
          </span>
        </Menu.Item>
        <Menu.Separator className="hx-cmenu__sep" />
        <Menu.Item className="hx-cmenu__item" data-danger="true" onClick={() => onAction(ACTION.remove)}>
          <Icon name="trash-2" size="sm" />
          <span className="hx-cmenu__txt">{t('next.connections.disconnect')}</span>
        </Menu.Item>
      </MenuPanel>
    </Menu.Root>
  );
};

/** The row's tools: «12 tools» folds out the names and what each does. Closed at first. */
const ToolList = ({ server }: { server: IMcpServer }) => {
  const [open, setOpen] = useState(false);
  const seen = useSeen(open);
  const id = useId();
  if (server.tools.length === 0) return null;
  return (
    <>
      <button
        type="button"
        className="hx-mcp-tools__toggle"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => setOpen((now) => !now)}
      >
        {toolsText(server.tools.length)}
        <Icon name="chevron-down" size="sm" className="hx-mcp-tools__chev" />
      </button>
      <div id={id} className="hx-reveal hx-mcp-tools" data-open={open}>
        <div className="hx-reveal__in" inert={!open || undefined}>
          {seen && (
            <ul
              className="hx-mcp-tools__list"
              aria-label={t('next.connections.mcp.toolsOf', { name: server.name })}
            >
              {server.tools.map((tool) => (
                <li key={tool.name} className="hx-mcp-tools__item">
                  <span className="hx-mcp-tools__name">{tool.name}</span>
                  {tool.description !== '' && <span className="hx-mcp-tools__desc">{tool.description}</span>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </>
  );
};

/** What the row says while its sign-in is under way: waiting for the popup, or the popup was blocked. */
const SignInLine = ({ signin, server }: { signin: ISignIn; server: IMcpServer }) => {
  const flow = signin.flow;
  if (flow === null || flow.id !== server.id || flow.phase === FLOW.opening) return null;
  if (flow.phase === FLOW.blocked && flow.authUrl !== null)
    return (
      <span className="hx-mcp-row__flow">
        {t('next.connections.mcp.blocked')}
        <a
          className="hx-mcp-row__link"
          href={flow.authUrl}
          target={POPUP_NAME}
          rel="opener"
          onClick={(event) => {
            if (signin.reopen()) event.preventDefault();
          }}
        >
          {t('next.connections.mcp.openSignIn')}
        </a>
      </span>
    );
  return (
    <span className="hx-mcp-row__flow">
      {t('next.connections.mcp.waiting')}
      <button type="button" className="hx-mcp-row__link" onClick={signin.cancel}>
        {t('next.connections.cancel')}
      </button>
    </span>
  );
};

/** A server just kept that asked for a token: its row opens the token form at once. */
interface ITokenAsk {
  id: string;
  headerName: string;
  tokenUrl: string;
}

/** After POST /mcp: the popup's sign-in, the token form, or nothing when the server connected at once. */
const NEXT = { signIn: 'signIn', token: 'token', none: 'none' } as const;
type TNext = (typeof NEXT)[keyof typeof NEXT];

const nextStep = (added: IMcpAdded): TNext => {
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

interface IRowProps {
  server: IMcpServer;
  /** Where its token is made (the catalog's page), or empty. */
  tokenUrl: string;
  /** The token the server asked for when it was kept, when it did. */
  ask: ITokenAsk | null;
  leaving: boolean;
  actions: IMcpActions;
  signin: ISignIn;
}

const firstHeader = (server: IMcpServer, ask: ITokenAsk | null): string => {
  if (ask !== null && ask.headerName !== '') return ask.headerName;
  return server.headerName === '' ? DEFAULT_HEADER : server.headerName;
};

const McpRow = ({ server, tokenUrl, ask, leaving, actions, signin }: IRowProps) => {
  const [panel, setPanel] = useState<TPanel | null>(ask === null ? null : PANEL.token);
  const [draft, setDraft] = useState(server.name);
  const [header, setHeader] = useState(() => firstHeader(server, ask));
  const [value, setValue] = useState('');
  const [busy, setBusy] = useState<TAction | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [kept, setKept] = useState('');
  const [checked, setChecked] = useState(false);
  const alive = useRef(true);
  const field = useRef<HTMLInputElement>(null);
  const ids = useId();
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (panel === PANEL.rename) field.current?.focus();
  }, [panel]);
  useEffect(() => {
    if (!checked) return;
    const timer = window.setTimeout(() => setChecked(false), CHECKED_MS);
    return () => window.clearTimeout(timer);
  }, [checked]);

  const flow = signin.flow !== null && signin.flow.id === server.id ? signin.flow : null;
  const failure = signin.failure !== null && signin.failure.id === server.id ? signin.failure.reason : null;
  const shownError = error ?? failure;
  if (shownError !== null && kept !== shownError) setKept(shownError);

  const problem = flow === null ? mcpStateText(server) : null;
  const asksPerson = flow === null && needsPerson(server);
  const signsIn = !takesToken(server);
  const working = busy !== null || (flow !== null && flow.phase === FLOW.opening);

  const run = async (action: TAction, call: () => TDone): Promise<boolean> => {
    setBusy(action);
    setError(null);
    setChecked(false);
    const refused = await call();
    if (!alive.current) return false;
    setBusy(null);
    setError(refused);
    return refused === null;
  };

  const pick = (action: TAction) => {
    if (action === ACTION.check) {
      void run(action, () => actions.onCheck(server.id)).then((ok) => alive.current && setChecked(ok));
      return;
    }
    if (action === ACTION.toggle) {
      void run(action, () => actions.onEnable(server.id, !server.enabled));
      return;
    }
    setError(null);
    if (action === ACTION.rename) {
      setDraft(server.name);
      setPanel(PANEL.rename);
      return;
    }
    if (action === ACTION.token) {
      setValue('');
      setPanel(PANEL.token);
      return;
    }
    setPanel(PANEL.remove);
  };

  /** «Sign in again»: the popup opens in this click; the machine then says where it goes. */
  const signIn = () => {
    setError(null);
    void signin.start(server.id, signin.prepare());
  };

  const rename = async () => {
    const next = draft.trim();
    if (next === '' || next === server.name) {
      setPanel(null);
      return;
    }
    if (await run(ACTION.rename, () => actions.onRename(server.id, next))) setPanel(null);
  };

  const sendToken = async () => {
    if (value.trim() === '' || busy !== null) return;
    const sent = headerOf(header, value);
    setValue('');
    if (await run(ACTION.token, () => actions.onHeader(server.id, sent))) setPanel(null);
  };

  const close = () => {
    setPanel(null);
    setValue('');
    setError(null);
  };

  return (
    <div className="hx-reveal hx-conn-wrap" data-open={!leaving}>
      <div className="hx-reveal__in">
        <div
          className="hx-conn-row hx-mcp-row"
          data-off={!server.enabled || undefined}
          data-busy={working || undefined}
          aria-busy={working || undefined}
        >
          <div className="hx-conn-row__head">
            <McpGlyph id={server.catalogId} name={server.name} />
            <div className="hx-conn-row__text">
              <span className="hx-conn-row__name">{server.name}</span>
              <span className="hx-conn-row__sub">{mcpSubLine(server)}</span>
              {problem !== null && (
                <span className="hx-conn-row__state" data-tone={isQuietState(server) ? 'quiet' : undefined}>
                  {!isQuietState(server) && <Icon name="triangle-alert" size="sm" />}
                  {problem}
                </span>
              )}
              <SignInLine signin={signin} server={server} />
              {checked && problem === null && (
                <span className="hx-conn-row__ok">
                  <Icon name="check" size="sm" />
                  {t('next.connections.works')}
                </span>
              )}
              <ToolList server={server} />
            </div>
            {asksPerson && signsIn && (
              <SetButton tone="ghost" icon="log-in" disabled={leaving || working} onClick={signIn}>
                {server.tools.length > 0
                  ? t('next.connections.mcp.signInAgain')
                  : t('next.connections.mcp.signIn')}
              </SetButton>
            )}
            {asksPerson && !signsIn && panel !== PANEL.token && (
              <SetButton
                tone="ghost"
                icon="key-round"
                disabled={leaving || working}
                onClick={() => pick(ACTION.token)}
              >
                {t('next.connections.mcp.enterToken')}
              </SetButton>
            )}
            {working && <Icon name="loader-circle" size="sm" className="hx-conn-row__spin" />}
            <RowMenu server={server} disabled={leaving || working} onAction={pick} />
          </div>
          <Reveal open={panel === PANEL.token && !leaving}>
            {panel === PANEL.token && (
              <div className="hx-conn-row__panel">
                <HeaderFields
                  name={header}
                  value={value}
                  tokenUrl={ask !== null && ask.tokenUrl !== '' ? ask.tokenUrl : tokenUrl}
                  disabled={busy !== null}
                  invalid={error !== null}
                  onName={setHeader}
                  onValue={(next) => {
                    setValue(next);
                    setError(null);
                  }}
                  onSubmit={() => void sendToken()}
                />
                <div className="hx-conn-row__buttons">
                  <SetButton
                    tone="primary"
                    busy={busy === ACTION.token}
                    disabled={value.trim() === ''}
                    onClick={() => void sendToken()}
                  >
                    {t('next.connections.save')}
                  </SetButton>
                  <SetButton tone="ghost" disabled={busy === ACTION.token} onClick={close}>
                    {t('next.connections.cancel')}
                  </SetButton>
                </div>
              </div>
            )}
          </Reveal>
          <Reveal open={panel === PANEL.rename && !leaving}>
            <div className="hx-conn-row__panel">
              <label className="hx-conn-form__label" htmlFor={`${ids}-name`}>
                {t('next.connections.name')}
              </label>
              <input
                id={`${ids}-name`}
                ref={field}
                className="hx-set__input"
                value={draft}
                maxLength={80}
                autoComplete="off"
                disabled={busy !== null}
                onChange={(event) => {
                  setDraft(event.target.value);
                  setError(null);
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') void rename();
                  if (event.key === 'Escape') close();
                }}
              />
              <div className="hx-conn-row__buttons">
                <SetButton
                  tone="primary"
                  busy={busy === ACTION.rename}
                  disabled={draft.trim() === ''}
                  onClick={() => void rename()}
                >
                  {t('next.connections.save')}
                </SetButton>
                <SetButton tone="ghost" onClick={close}>
                  {t('next.connections.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={panel === PANEL.remove && !leaving}>
            <div className="hx-conn-row__panel">
              <p className="hx-conn-row__confirm">
                {t('next.connections.mcp.disconnectSure', { name: server.name })}
              </p>
              <div className="hx-conn-row__buttons">
                <SetButton
                  tone="danger"
                  busy={busy === ACTION.remove}
                  onClick={() => void run(ACTION.remove, () => actions.onRemove(server.id))}
                >
                  {t('next.connections.disconnect')}
                </SetButton>
                <SetButton tone="ghost" disabled={busy === ACTION.remove} onClick={close}>
                  {t('next.connections.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={shownError !== null && !leaving}>
            <div className="hx-conn-row__err">
              <FormError>{kept}</FormError>
            </div>
          </Reveal>
        </div>
      </div>
    </div>
  );
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

const McpSection = ({
  mcp,
  failed = null,
  onProbe,
  onAdd,
  onSignIn,
  onRefresh,
  ...actions
}: IMcpSectionProps) => {
  const servers = mcp === null ? null : mcp.servers;
  const rows = useLeavingList(servers ?? NO_SERVERS, (server) => server.id, LEAVE_MS);
  const catalog = useLeavingList(
    mcp === null ? [] : openCatalog(mcp.catalog, mcp.servers),
    (entry) => entry.id,
    LEAVE_MS,
  );
  const signin = useMcpSignIn({ servers, onStart: onSignIn, onRefresh, reasonText: mcpReasonText });
  const { prepare, release, start } = signin;
  const [ask, setAsk] = useState<ITokenAsk | null>(null);
  const [picked, setPicked] = useState<IPicked | null>(null);
  const [all, setAll] = useState(false);
  const form = useRef<HTMLDivElement>(null);

  const pick = (entry: IPicked['entry']) => {
    setPicked({ entry, at: Date.now() });
    form.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  };

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
        <Head />
        <Group>
          {failed === null && <RowsSkeleton />}
          {failed !== null && (
            <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
          )}
        </Group>
      </div>
    );

  return (
    <div className="hx-mcp">
      <Head />
      {rows.length > 0 && (
        <Group>
          {rows.map(({ item, leaving }) => (
            <McpRow
              key={item.id}
              server={item}
              tokenUrl={mcp.catalog.find((entry) => entry.id === item.catalogId)?.tokenUrl ?? ''}
              ask={ask !== null && ask.id === item.id ? ask : null}
              leaving={leaving}
              actions={actions}
              signin={signin}
            />
          ))}
        </Group>
      )}
      <Group>
        <ConnectForm
          ref={form}
          picked={picked}
          onProbe={onProbe}
          onConnect={connect}
          onUnpick={() => setPicked(null)}
        />
      </Group>
      {catalog.length > 0 && (
        <div className="hx-mcp-cat">
          {(all ? catalog : catalog.slice(0, CATALOG_FIRST)).map(({ item, leaving }) => (
            <CatalogCard
              key={item.id}
              entry={item}
              leaving={leaving}
              picked={picked !== null && picked.entry.id === item.id}
              onPick={pick}
            />
          ))}
        </div>
      )}
      {!all && catalog.length > CATALOG_FIRST && (
        <button type="button" className="hx-mcp-more" onClick={() => setAll(true)}>
          {t('next.connections.mcp.more')}
          <Icon name="chevron-down" size="sm" />
        </button>
      )}
      <p className="hx-set__foot hx-mcp__foot">{t('next.connections.mcp.foot')}</p>
    </div>
  );
};

export default McpSection;
