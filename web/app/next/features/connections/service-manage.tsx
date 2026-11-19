// The state of a connected server on its page and what the person can do with it: check it again, sign in again, give a
// new token or keys, rename, turn it off for agents, disconnect. Every action is a button in plain sight; the ones that
// need a word from the person (a token, keys, a name, a «sure?») open a panel under them. A refused action says why
// right there.
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import {
  DEFAULT_HEADER,
  type IMcpHeader,
  type IMcpServer,
  type IOwnDescription,
} from '@/next/features/connections/mcp-api';
import {
  EnvFields,
  HeaderFields,
  headerOf,
  StartingHint,
  useEnvValues,
} from '@/next/features/connections/mcp-catalog';
import {
  envPayload,
  isKeyed,
  mcpSubLine,
  needsPerson,
  takesToken,
} from '@/next/features/connections/mcp-model';
import { FLOW, POPUP_NAME, type ISignIn } from '@/next/features/connections/mcp-signin';
import { statusOf } from '@/next/features/connections/service-model';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

/** «Works» after «Check» stays this long. */
const CHECKED_MS = 2500;

const PANEL = { rename: 'rename', remove: 'remove', token: 'token', keys: 'keys' } as const;
type TPanel = (typeof PANEL)[keyof typeof PANEL];

const ACTION = {
  check: 'check',
  rename: 'rename',
  toggle: 'toggle',
  remove: 'remove',
  token: 'token',
  keys: 'keys',
} as const;
type TAction = (typeof ACTION)[keyof typeof ACTION];

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export interface IMcpActions {
  onCheck: (id: string) => TDone;
  onRename: (id: string, name: string) => TDone;
  onEnable: (id: string, enabled: boolean) => TDone;
  onRemove: (id: string) => TDone;
  onHeader: (id: string, header: IMcpHeader) => TDone;
  /** New keys of a local server or an API; a key left out stays as it is. */
  onEnv: (id: string, env: Readonly<Record<string, string>>) => TDone;
  /** The hand-written description of a service of one's own. */
  onDescribe?: (id: string, description: IOwnDescription) => TDone;
}

/** A server just kept that asked for a token: its card opens the token form at once. */
export interface ITokenAsk {
  id: string;
  headerName: string;
  tokenUrl: string;
}

const firstHeader = (server: IMcpServer, ask: ITokenAsk | null): string => {
  if (ask !== null && ask.headerName !== '') return ask.headerName;
  return server.headerName === '' ? DEFAULT_HEADER : server.headerName;
};

const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

/** What the card says while its sign-in is under way: waiting for the popup, or the popup was blocked. */
const SignInLine = ({ signin, server }: { signin: ISignIn; server: IMcpServer }) => {
  const flow = signin.flow;
  if (flow === null || flow.id !== server.id || flow.phase === FLOW.opening) return null;
  if (flow.phase === FLOW.blocked && flow.authUrl !== null)
    return (
      <p className="hx-svc-manage__flow">
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
      </p>
    );
  return (
    <p className="hx-svc-manage__flow">
      {t('next.connections.mcp.waiting')}
      <button type="button" className="hx-mcp-row__link" onClick={signin.cancel}>
        {t('next.connections.cancel')}
      </button>
    </p>
  );
};

interface IManageProps {
  server: IMcpServer;
  /** Where its token is made (the catalog's page), or empty. */
  tokenUrl: string;
  /** What goes before a bare token instead of «Bearer » (the catalog's), or empty. */
  tokenPrefix: string;
  /** The token the server asked for when it was kept, when it did. */
  ask: ITokenAsk | null;
  actions: IMcpActions;
  signin: ISignIn;
  /** The server is gone: the page leaves it. */
  onGone: () => void;
}

const ServerManage = ({ server, tokenUrl, tokenPrefix, ask, actions, signin, onGone }: IManageProps) => {
  const [panel, setPanel] = useState<TPanel | null>(ask === null ? null : PANEL.token);
  const [draft, setDraft] = useState(server.name);
  const [header, setHeader] = useState(() => firstHeader(server, ask));
  const [value, setValue] = useState('');
  const env = useEnvValues(server.env, false);
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

  const status = statusOf(server);
  const asksPerson = flow === null && needsPerson(server);
  const keyed = isKeyed(server) && server.env.length > 0;
  const signsIn = !takesToken(server) && !isKeyed(server);
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

  const open = (next: TPanel) => {
    setError(null);
    if (next === PANEL.rename) setDraft(server.name);
    if (next === PANEL.token) setValue('');
    if (next === PANEL.keys) env.clear();
    setPanel(next);
  };

  const close = () => {
    setPanel(null);
    env.clear();
    setValue('');
    setError(null);
  };

  /** «Sign in again»: the popup opens in this click; the machine then says where it goes. */
  const signIn = () => {
    setError(null);
    void signin.start(server.id, signin.prepare());
  };

  const check = () => {
    void run(ACTION.check, () => actions.onCheck(server.id)).then((ok) => alive.current && setChecked(ok));
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
    const sent = headerOf(header, value, tokenPrefix);
    setValue('');
    if (await run(ACTION.token, () => actions.onHeader(server.id, sent))) setPanel(null);
  };

  const keysSent = envPayload(server.env, env.values);
  const sendKeys = async () => {
    if (Object.keys(keysSent).length === 0 || busy !== null) return;
    env.clear();
    if (await run(ACTION.keys, () => actions.onEnv(server.id, keysSent))) setPanel(null);
  };

  const remove = async () => {
    if (await run(ACTION.remove, () => actions.onRemove(server.id))) onGone();
  };

  return (
    <section className="hx-svc-card hx-svc-manage" aria-busy={working || undefined} data-tone={status.tone}>
      <div className="hx-svc-manage__top">
        <span className="hx-svc-pill" data-tone={status.tone}>
          <span className="hx-svc-pill__dot" aria-hidden="true" />
          {status.label}
        </span>
        <span className="hx-svc-manage__sub">{mcpSubLine(server)}</span>
        {working && <Icon name="loader-circle" size="sm" className="hx-conn-row__spin" />}
      </div>
      {status.detail !== null && flow === null && <p className="hx-svc-manage__detail">{status.detail}</p>}
      <SignInLine signin={signin} server={server} />
      {checked && status.detail === null && (
        <p className="hx-svc-manage__ok">
          <Icon name="check" size="sm" />
          {t('next.connections.works')}
        </p>
      )}
      <div className="hx-svc-manage__buttons">
        {asksPerson && signsIn && (
          <SetButton tone="primary" icon="log-in" disabled={working} onClick={signIn}>
            {server.tools.length > 0
              ? t('next.connections.mcp.signInAgain')
              : t('next.connections.mcp.signIn')}
          </SetButton>
        )}
        {asksPerson && keyed && panel !== PANEL.keys && (
          <SetButton tone="primary" icon="key-round" disabled={working} onClick={() => open(PANEL.keys)}>
            {t('next.connections.mcp.keys')}
          </SetButton>
        )}
        {asksPerson && !signsIn && !isKeyed(server) && panel !== PANEL.token && (
          <SetButton tone="primary" icon="key-round" disabled={working} onClick={() => open(PANEL.token)}>
            {t('next.connections.mcp.enterToken')}
          </SetButton>
        )}
        <SetButton
          tone="ghost"
          icon="refresh-cw"
          busy={busy === ACTION.check}
          disabled={working}
          onClick={check}
        >
          {t('next.connections.svc.check')}
        </SetButton>
        {!asksPerson && keyed && (
          <SetButton tone="ghost" icon="key-round" disabled={working} onClick={() => open(PANEL.keys)}>
            {t('next.connections.mcp.keys')}
          </SetButton>
        )}
        {!asksPerson && !signsIn && !isKeyed(server) && (
          <SetButton tone="ghost" icon="key-round" disabled={working} onClick={() => open(PANEL.token)}>
            {t('next.connections.svc.newToken')}
          </SetButton>
        )}
        <SetButton
          tone="ghost"
          icon={server.enabled ? 'circle-stop' : 'play'}
          busy={busy === ACTION.toggle}
          disabled={working}
          onClick={() => void run(ACTION.toggle, () => actions.onEnable(server.id, !server.enabled))}
        >
          {server.enabled ? t('next.connections.svc.turnOff') : t('next.connections.svc.turnOn')}
        </SetButton>
        <SetButton tone="ghost" icon="pencil" disabled={working} onClick={() => open(PANEL.rename)}>
          {t('next.connections.rename')}
        </SetButton>
        <SetButton tone="danger" icon="trash-2" disabled={working} onClick={() => open(PANEL.remove)}>
          {t('next.connections.disconnect')}
        </SetButton>
      </div>
      <Reveal open={panel === PANEL.token}>
        {panel === PANEL.token && (
          <div className="hx-svc-manage__panel">
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
            <div className="hx-svc-manage__buttons">
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
      <Reveal open={panel === PANEL.keys}>
        {panel === PANEL.keys && (
          <div className="hx-svc-manage__panel">
            <EnvFields
              fields={server.env}
              values={env.values}
              keep
              disabled={busy !== null}
              invalid={error !== null}
              onChange={(name, next) => {
                env.set(name, next);
                setError(null);
              }}
              onSubmit={() => void sendKeys()}
            />
            {busy === ACTION.keys && <StartingHint />}
            <div className="hx-svc-manage__buttons">
              <SetButton
                tone="primary"
                busy={busy === ACTION.keys}
                disabled={Object.keys(keysSent).length === 0}
                onClick={() => void sendKeys()}
              >
                {t('next.connections.save')}
              </SetButton>
              <SetButton tone="ghost" disabled={busy === ACTION.keys} onClick={close}>
                {t('next.connections.cancel')}
              </SetButton>
            </div>
          </div>
        )}
      </Reveal>
      <Reveal open={panel === PANEL.rename}>
        <div className="hx-svc-manage__panel">
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
          <div className="hx-svc-manage__buttons">
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
      <Reveal open={panel === PANEL.remove}>
        <div className="hx-svc-manage__panel">
          <p className="hx-svc-manage__confirm">
            {t('next.connections.mcp.disconnectSure', { name: server.name })}
          </p>
          <div className="hx-svc-manage__buttons">
            <SetButton tone="danger" busy={busy === ACTION.remove} onClick={() => void remove()}>
              {t('next.connections.disconnect')}
            </SetButton>
            <SetButton tone="ghost" disabled={busy === ACTION.remove} onClick={close}>
              {t('next.connections.cancel')}
            </SetButton>
          </div>
        </div>
      </Reveal>
      <Reveal open={shownError !== null}>
        <div className="hx-svc-manage__panel">
          <FormError>{kept}</FormError>
        </div>
      </Reveal>
    </section>
  );
};

export default ServerManage;
